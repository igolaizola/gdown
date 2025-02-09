package gdown

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

//
// Constants and error variables
//

const (
	CHUNK_SIZE       = 512 * 1024
	MAX_NUMBER_FILES = 50
)

var (
	// ErrFileURLRetrieval is returned when a file’s public URL cannot be determined.
	ErrFileURLRetrieval = errors.New("failed to retrieve file URL")
)

//
// DownloadOptions and FolderOptions hold settings for downloads.
//

type DownloadOptions struct {
	Quiet      bool
	Proxy      string
	Speed      int64 // bytes per second; 0 means unlimited
	UseCookies bool
	Verify     bool
	Resume     bool
	Fuzzy      bool
	Format     string
	UserAgent  string
}

type FolderOptions struct {
	DownloadOptions
	RemainingOk bool
}

//
// ThrottledWriter implements a writer that sleeps as needed to limit download speed.
//

type ThrottledWriter struct {
	writer  io.Writer
	speed   int64 // bytes per second
	start   time.Time
	written int64
}

func NewThrottledWriter(w io.Writer, speed int64) *ThrottledWriter {
	return &ThrottledWriter{
		writer:  w,
		speed:   speed,
		start:   time.Now(),
		written: 0,
	}
}

func (tw *ThrottledWriter) Write(p []byte) (n int, err error) {
	n, err = tw.writer.Write(p)
	tw.written += int64(n)
	elapsed := time.Since(tw.start)
	// Calculate expected elapsed time
	expected := time.Duration(float64(tw.written)/float64(tw.speed)) * time.Second
	if expected > elapsed {
		time.Sleep(expected - elapsed)
	}
	return
}

//
// Helper functions
//

// fileExists returns true if filename exists and is not a directory.
func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	return err == nil && !info.IsDir()
}

// sanitizeFilename replaces OS path separator characters with underscores.
func sanitizeFilename(name string) string {
	return strings.ReplaceAll(name, string(os.PathSeparator), "_")
}

// getCacheRoot returns the cache directory for downloads.
func getCacheRoot() string {
	usr, err := user.Current()
	if err != nil {
		return "."
	}
	return filepath.Join(usr.HomeDir, ".cache", "gdown")
}

//
// HTTP client creation (used in downloads)
//

func newHTTPClient(opts DownloadOptions) (*http.Client, error) {
	transport := &http.Transport{}
	if opts.Proxy != "" {
		proxyURL, err := url.Parse(opts.Proxy)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	// In Go, TLS is verified by default; disable via InsecureSkipVerify if needed.
	if !opts.Verify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	var jar http.CookieJar
	if opts.UseCookies {
		var err error
		jar, err = cookiejar.New(nil)
		if err != nil {
			return nil, err
		}
	}
	client := &http.Client{
		Transport: transport,
		Jar:       jar,
		Timeout:   0,
	}
	return client, nil
}

//
// MD5 hash functions (from cached_download.py)
//

func MD5Sum(filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func assertFileHash(filename, expectedHash string, quiet bool) (bool, error) {
	parts := strings.SplitN(expectedHash, ":", 2)
	if len(parts) != 2 {
		return false, fmt.Errorf("invalid hash format: %s", expectedHash)
	}
	algo := parts[0]
	expected := parts[1]
	var actual string
	switch algo {
	case "md5":
		sum, err := MD5Sum(filename)
		if err != nil {
			return false, err
		}
		actual = sum
	default:
		return false, fmt.Errorf("unsupported hash algorithm: %s", algo)
	}
	if actual == expected {
		if !quiet {
			fmt.Fprintf(os.Stderr, "Hash matches: %s == %s\n", actual, expected)
		}
		return true, nil
	}
	return false, fmt.Errorf("hash mismatch: actual %s, expected %s", actual, expected)
}

//
// Download() – downloads a file from URL (adapted from download.py)
//

func Download(urlStr, output string, opts DownloadOptions) (string, error) {
	if opts.UserAgent == "" {
		opts.UserAgent = "Mozilla/5.0 (compatible; gdown-go)"
	}
	client, err := newHTTPClient(opts)
	if err != nil {
		return "", err
	}

	origUrl := urlStr
	for {
		var startSize int64 = 0
		if opts.Resume && fileExists(output) {
			if fi, err := os.Stat(output); err == nil {
				startSize = fi.Size()
			}
		}
		req, err := http.NewRequest("GET", urlStr, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", opts.UserAgent)
		if startSize > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startSize))
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}

		// If HTML, try to extract a confirmation download URL.
		ct := resp.Header.Get("Content-Type")
		if strings.HasPrefix(ct, "text/html") {
			bodyBytes, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return "", err
			}
			newUrl, err := getUrlFromGDriveConfirmation(string(bodyBytes))
			if err != nil {
				return "", err
			}
			urlStr = newUrl
			if origUrl == urlStr {
				break
			}
			continue
		}

		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return "", fmt.Errorf("HTTP error: %s", resp.Status)
		}

		// If output is empty, use the basename from the URL.
		if output == "" {
			u, err := url.Parse(urlStr)
			if err != nil {
				return "", err
			}
			output = path.Base(u.Path)
		}
		// If output is a directory, get filename from response.
		if fi, err := os.Stat(output); err == nil && fi.IsDir() {
			fname := getFilenameFromResponse(resp)
			output = filepath.Join(output, fname)
		}
		// Open file (append if resuming).
		var file *os.File
		if opts.Resume {
			file, err = os.OpenFile(output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		} else {
			file, err = os.Create(output)
		}
		if err != nil {
			return "", err
		}
		defer file.Close()

		var writer io.Writer = file
		if opts.Speed > 0 {
			writer = NewThrottledWriter(file, opts.Speed)
		}
		if !opts.Quiet {
			fmt.Fprintf(os.Stderr, "Downloading %s to %s\n", urlStr, output)
		}
		buf := make([]byte, CHUNK_SIZE)
		_, err = io.CopyBuffer(writer, resp.Body, buf)
		if err != nil {
			return "", err
		}
		return output, nil
	}
	return output, nil
}

// getFilenameFromResponse extracts a filename from the Content-Disposition header.
func getFilenameFromResponse(resp *http.Response) string {
	cd := resp.Header.Get("Content-Disposition")
	if cd != "" {
		re := regexp.MustCompile(`filename\*=UTF-8''(.+)`)
		matches := re.FindStringSubmatch(cd)
		if len(matches) == 2 {
			filename, err := url.QueryUnescape(matches[1])
			if err == nil {
				return sanitizeFilename(filename)
			}
		}
		re = regexp.MustCompile(`attachment; filename="(.*?)"`)
		matches = re.FindStringSubmatch(cd)
		if len(matches) == 2 {
			return sanitizeFilename(matches[1])
		}
	}
	return "downloaded_file"
}

// getUrlFromGDriveConfirmation scans HTML for a confirmation download link.
func getUrlFromGDriveConfirmation(html string) (string, error) {
	re := regexp.MustCompile(`href="(\/uc\?export=download[^"]+)"`)
	matches := re.FindStringSubmatch(html)
	if len(matches) == 2 {
		urlStr := "https://docs.google.com" + matches[1]
		urlStr = strings.ReplaceAll(urlStr, "&amp;", "&")
		return urlStr, nil
	}
	return "", ErrFileURLRetrieval
}

//
// CachedDownload() – downloads a file to a cache directory (from cached_download.py)
//

func CachedDownload(urlStr, outputPath, hash string, quiet bool, postprocess func(string) error, opts DownloadOptions) (string, error) {
	cacheRoot := getCacheRoot()
	_ = os.MkdirAll(cacheRoot, os.ModePerm)
	if outputPath == "" {
		// Sanitize the URL to use as a filename.
		sanitized := strings.NewReplacer("/", "-SLASH-", ":", "-COLON-", "=", "-EQUAL-", "?", "-QUESTION-").Replace(urlStr)
		outputPath = filepath.Join(cacheRoot, sanitized)
	}
	if fileExists(outputPath) && hash == "" {
		if !quiet {
			fmt.Fprintf(os.Stderr, "File exists: %s\n", outputPath)
		}
		return outputPath, nil
	} else if fileExists(outputPath) && hash != "" {
		if ok, _ := assertFileHash(outputPath, hash, quiet); ok {
			return outputPath, nil
		}
		fmt.Fprintf(os.Stderr, "Hash mismatch, redownloading: %s\n", outputPath)
	}
	tmpDir, err := os.MkdirTemp(cacheRoot, "dl")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)
	tempPath := filepath.Join(tmpDir, "dl")
	downloadedPath, err := Download(urlStr, tempPath, opts)
	if err != nil {
		return "", err
	}
	err = os.Rename(downloadedPath, outputPath)
	if err != nil {
		return "", err
	}
	if hash != "" {
		if ok, err := assertFileHash(outputPath, hash, quiet); err != nil || !ok {
			return "", fmt.Errorf("hash mismatch for file %s", outputPath)
		}
	}
	if postprocess != nil {
		if err := postprocess(outputPath); err != nil {
			return "", err
		}
	}
	return outputPath, nil
}

//
// URL parsing helpers (from parse_url.py)
//

func IsGoogleDriveUrl(urlStr string) bool {
	u, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "drive.google.com" || host == "docs.google.com"
}

// ParseUrl extracts a Google Drive file ID (if any) from the URL.
func ParseUrl(urlStr string, warn bool) (fileId string, isDownloadLink bool, err error) {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return "", false, err
	}
	queryParams := parsed.Query()
	isGDrive := IsGoogleDriveUrl(urlStr)
	isDownloadLink = strings.HasSuffix(parsed.Path, "/uc")
	if !isGDrive {
		return "", false, nil
	}
	if queryParams.Has("id") {
		fileId = queryParams.Get("id")
		return fileId, isDownloadLink, nil
	}
	patterns := []string{
		`^/file/d/(.*?)/(edit|view)$`,
		`^/file/u/[0-9]+/d/(.*?)/(edit|view)$`,
		`^/document/d/(.*?)/(edit|htmlview|view)$`,
		`^/document/u/[0-9]+/d/(.*?)/(edit|htmlview|view)$`,
		`^/presentation/d/(.*?)/(edit|htmlview|view)$`,
		`^/presentation/u/[0-9]+/d/(.*?)/(edit|htmlview|view)$`,
		`^/spreadsheets/d/(.*?)/(edit|htmlview|view)$`,
		`^/spreadsheets/u/[0-9]+/d/(.*?)/(edit|htmlview|view)$`,
	}
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(parsed.Path)
		if len(matches) >= 2 {
			fileId = matches[1]
			break
		}
	}
	if warn && fileId != "" && !isDownloadLink {
		fmt.Fprintln(os.Stderr, "Warning: You specified a Google Drive link that is not a direct download link. Consider using fuzzy matching.")
	}
	return fileId, isDownloadLink, nil
}

//
// Archive extraction (from extractall.py)
//

func ExtractAll(archivePath, to string) ([]string, error) {
	if to == "" {
		to = filepath.Dir(archivePath)
	}
	var extractedFiles []string
	if strings.HasSuffix(archivePath, ".zip") {
		r, err := zip.OpenReader(archivePath)
		if err != nil {
			return nil, err
		}
		defer r.Close()
		for _, f := range r.File {
			fpath := filepath.Join(to, f.Name)
			if f.FileInfo().IsDir() {
				_ = os.MkdirAll(fpath, os.ModePerm)
				continue
			}
			_ = os.MkdirAll(filepath.Dir(fpath), os.ModePerm)
			outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return nil, err
			}
			rc, err := f.Open()
			if err != nil {
				outFile.Close()
				return nil, err
			}
			_, err = io.Copy(outFile, rc)
			outFile.Close()
			rc.Close()
			if err != nil {
				return nil, err
			}
			extractedFiles = append(extractedFiles, fpath)
		}
		return extractedFiles, nil
	} else if strings.HasSuffix(archivePath, ".tar") ||
		strings.HasSuffix(archivePath, ".tar.gz") ||
		strings.HasSuffix(archivePath, ".tgz") {
		f, err := os.Open(archivePath)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		var tarReader *tar.Reader
		if strings.HasSuffix(archivePath, ".tar") {
			tarReader = tar.NewReader(f)
		} else {
			gz, err := gzip.NewReader(f)
			if err != nil {
				return nil, err
			}
			defer gz.Close()
			tarReader = tar.NewReader(gz)
		}
		for {
			header, err := tarReader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			fpath := filepath.Join(to, header.Name)
			switch header.Typeflag {
			case tar.TypeDir:
				_ = os.MkdirAll(fpath, os.ModePerm)
			case tar.TypeReg:
				_ = os.MkdirAll(filepath.Dir(fpath), os.ModePerm)
				outFile, err := os.Create(fpath)
				if err != nil {
					return nil, err
				}
				if _, err := io.Copy(outFile, tarReader); err != nil {
					outFile.Close()
					return nil, err
				}
				outFile.Close()
				extractedFiles = append(extractedFiles, fpath)
			}
		}
		return extractedFiles, nil
	} else {
		return nil, fmt.Errorf("unsupported archive format: %s", archivePath)
	}
}

//
// Google Drive folder download support (from download_folder.py)
//

// GoogleDriveFile represents a file or folder on Google Drive.
type GoogleDriveFile struct {
	ID       string
	Name     string
	Type     string
	Children []*GoogleDriveFile
}

func (f *GoogleDriveFile) IsFolder() bool {
	return f.Type == "application/vnd.google-apps.folder"
}

// decodeUnicodeEscapes converts any \xHH sequences into \u00HH sequences
// and then uses json.Unmarshal to decode the resulting JSON string.
func decodeUnicodeEscapes(s string) (string, error) {
	// Replace all \xHH with \u00HH.
	re := regexp.MustCompile(`\\x([0-9A-Fa-f]{2})`)
	s = re.ReplaceAllString(s, `\u00$1`)
	// Wrap in quotes to form a valid JSON string literal.
	quoted := `"` + s + `"`
	var decoded string
	if err := json.Unmarshal([]byte(quoted), &decoded); err != nil {
		return "", err
	}
	return decoded, nil
}

// parseGoogleDriveFile parses HTML content to extract folder information.
func parseGoogleDriveFile(urlStr, content string) (*GoogleDriveFile, []struct {
	ID   string
	Name string
	Type string
}, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(content))
	if err != nil {
		return nil, nil, err
	}

	var encodedData string
	doc.Find("script").EachWithBreak(func(i int, s *goquery.Selection) bool {
		rawHTML, _ := s.Html()
		htmlContent := html.UnescapeString(rawHTML)
		if strings.Contains(htmlContent, "_DRIVE_ivd") {
			re := regexp.MustCompile(`'((?:[^'\\]|\\.)*)'`)
			matches := re.FindAllStringSubmatch(htmlContent, -1)
			if len(matches) >= 2 {
				encodedData = matches[1][1]
				return false
			}
		}
		return true
	})
	if encodedData == "" {
		// Write the HTML to a file for debugging.
		return nil, nil, errors.New("could not find the folder encoded JS string")
	}
	decoded, err := decodeUnicodeEscapes(encodedData)
	if err != nil {
		return nil, nil, fmt.Errorf("couldn't decode unicode escapes: %w", err)
	}
	var folderArr []interface{}
	if err := json.Unmarshal([]byte(decoded), &folderArr); err != nil {
		return nil, nil, err
	}
	var folderContents []interface{}
	if len(folderArr) > 0 && folderArr[0] != nil {
		if arr, ok := folderArr[0].([]interface{}); ok {
			folderContents = arr
		}
	}

	title := doc.Find("title").First().Text()
	sep := " - "
	parts := strings.Split(title, sep)
	if len(parts) < 2 {
		return nil, nil, fmt.Errorf("folder name cannot be extracted from title: %s", title)
	}
	name := strings.Join(parts[:len(parts)-1], sep)
	gfile := &GoogleDriveFile{
		ID:   path.Base(urlStr),
		Name: name,
		Type: "application/vnd.google-apps.folder",
	}
	var children []struct {
		ID   string
		Name string
		Type string
	}
	for _, item := range folderContents {
		if arr, ok := item.([]interface{}); ok && len(arr) >= 4 {
			id, _ := arr[0].(string)
			nameEncoded, _ := arr[2].(string)
			typ, _ := arr[3].(string)
			children = append(children, struct {
				ID   string
				Name string
				Type string
			}{ID: id, Name: nameEncoded, Type: typ})
		}
	}
	return gfile, children, nil
}

// downloadAndParseGoogleDriveLink retrieves and parses a folder page.
func downloadAndParseGoogleDriveLink(client *http.Client, urlStr string, quiet bool, remainingOk, verify bool) (*GoogleDriveFile, error) {
	if IsGoogleDriveUrl(urlStr) {
		if strings.Contains(urlStr, "?") {
			urlStr += "&hl=en"
		} else {
			urlStr += "?hl=en"
		}
	}
	resp, err := client.Get(urlStr)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to retrieve folder contents, status: %s", resp.Status)
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	bodyStr := string(bodyBytes)
	gfile, children, err := parseGoogleDriveFile(urlStr, bodyStr)
	if err != nil {
		return nil, err
	}
	for _, child := range children {
		if child.Type != "application/vnd.google-apps.folder" {
			if !quiet {
				fmt.Fprintf(os.Stderr, "Processing file %s %s\n", child.ID, child.Name)
			}
			childFile := &GoogleDriveFile{
				ID:   child.ID,
				Name: child.Name,
				Type: child.Type,
			}
			gfile.Children = append(gfile.Children, childFile)
		} else {
			if !quiet {
				fmt.Fprintf(os.Stderr, "Retrieving folder %s %s\n", child.ID, child.Name)
			}
			subUrl := "https://drive.google.com/drive/folders/" + child.ID
			subFolder, err := downloadAndParseGoogleDriveLink(client, subUrl, quiet, remainingOk, verify)
			if err != nil {
				return nil, err
			}
			gfile.Children = append(gfile.Children, subFolder)
		}
	}
	if len(gfile.Children) == MAX_NUMBER_FILES && !remainingOk {
		return nil, fmt.Errorf("folder has more than %d files", MAX_NUMBER_FILES)
	}
	return gfile, nil
}

// FileToDownload holds information for a file (or folder) within a folder.
type FileToDownload struct {
	ID        string
	Path      string // relative path within the folder
	LocalPath string
}

func getDirectoryStructure(gfile *GoogleDriveFile, prevPath string) []FileToDownload {
	var files []FileToDownload
	for _, child := range gfile.Children {
		safeName := strings.ReplaceAll(child.Name, string(os.PathSeparator), "_")
		if child.IsFolder() {
			newPath := filepath.Join(prevPath, safeName)
			// Directory entry (ID empty)
			files = append(files, FileToDownload{ID: "", Path: newPath, LocalPath: newPath})
			subFiles := getDirectoryStructure(child, newPath)
			files = append(files, subFiles...)
		} else {
			filePath := filepath.Join(prevPath, safeName)
			files = append(files, FileToDownload{ID: child.ID, Path: filePath})
		}
	}
	return files
}

// New: FileInfo type and ListFolder function.
// FileInfo holds details about a file or folder within a Google Drive folder.
// For files, DownloadURL is provided so you can call Download individually.
type FileInfo struct {
	ID          string
	Path        string // relative path within the folder
	DownloadURL string // non-empty for files; empty for folders
	IsFolder    bool
}

// ListFolder retrieves a folder’s structure and returns a list of FileInfo.
// Either urlStr or id must be specified (but not both). For files, DownloadURL is set.
func ListFolder(urlStr, id string, opts FolderOptions) ([]FileInfo, error) {
	if (id == "" && urlStr == "") || (id != "" && urlStr != "") {
		return nil, fmt.Errorf("either url or id must be specified")
	}
	if id != "" {
		urlStr = "https://drive.google.com/drive/folders/" + id
	}
	if opts.UserAgent == "" {
		opts.UserAgent = "Mozilla/5.0 (compatible; gdown-go)"
	}
	client, err := newHTTPClient(opts.DownloadOptions)
	if err != nil {
		return nil, err
	}
	if !opts.Quiet {
		fmt.Fprintln(os.Stderr, "Retrieving folder contents")
	}
	gfile, err := downloadAndParseGoogleDriveLink(client, urlStr, opts.Quiet, opts.RemainingOk, opts.Verify)
	if err != nil {
		return nil, err
	}
	filesToDownload := getDirectoryStructure(gfile, "")
	var infos []FileInfo
	for _, f := range filesToDownload {
		info := FileInfo{
			ID:   f.ID,
			Path: f.Path,
		}
		if f.ID == "" {
			info.IsFolder = true
		} else {
			info.IsFolder = false
			info.DownloadURL = "https://drive.google.com/uc?id=" + f.ID
		}
		infos = append(infos, info)
	}
	return infos, nil
}

//
// DownloadFolder() – downloads an entire Google Drive folder (from download_folder.py)
//

func DownloadFolder(urlStr, id, output string, opts FolderOptions) ([]string, error) {
	if (id == "" && urlStr == "") || (id != "" && urlStr != "") {
		return nil, fmt.Errorf("either url or id must be specified")
	}
	if id != "" {
		urlStr = "https://drive.google.com/drive/folders/" + id
	}
	if opts.UserAgent == "" {
		opts.UserAgent = "Mozilla/5.0 (compatible; gdown-go)"
	}
	client, err := newHTTPClient(opts.DownloadOptions)
	if err != nil {
		return nil, err
	}
	if !opts.Quiet {
		fmt.Fprintln(os.Stderr, "Retrieving folder contents")
	}
	gfile, err := downloadAndParseGoogleDriveLink(client, urlStr, opts.Quiet, opts.RemainingOk, opts.Verify)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to retrieve folder contents")
		return nil, err
	}
	if !opts.Quiet {
		fmt.Fprintln(os.Stderr, "Building directory structure")
	}
	filesToDownload := getDirectoryStructure(gfile, "")
	if output == "" {
		cwd, _ := os.Getwd()
		output = cwd + string(os.PathSeparator)
	}
	var rootDir string
	if strings.HasSuffix(output, string(os.PathSeparator)) {
		rootDir = filepath.Join(output, gfile.Name)
	} else {
		rootDir = output
	}
	if !opts.Quiet {
		fmt.Fprintf(os.Stderr, "Creating directory %s\n", rootDir)
	}
	_ = os.MkdirAll(rootDir, os.ModePerm)
	var downloadedFiles []string
	for _, f := range filesToDownload {
		localPath := filepath.Join(rootDir, f.Path)
		if f.ID == "" { // folder
			_ = os.MkdirAll(localPath, os.ModePerm)
			continue
		}
		if opts.Resume && fileExists(localPath) {
			if !opts.Quiet {
				fmt.Fprintf(os.Stderr, "Skipping already downloaded file %s\n", localPath)
			}
			downloadedFiles = append(downloadedFiles, localPath)
			continue
		}
		fileUrl := "https://drive.google.com/uc?id=" + f.ID
		downloaded, err := Download(fileUrl, localPath, opts.DownloadOptions)
		if err != nil {
			return nil, err
		}
		downloadedFiles = append(downloadedFiles, downloaded)
	}
	if !opts.Quiet {
		fmt.Fprintln(os.Stderr, "Download completed")
	}
	return downloadedFiles, nil
}
