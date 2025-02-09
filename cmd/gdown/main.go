// main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"

	"github.com/igolaizola/gdown"
	"github.com/peterbourgon/ff/v3"
	"github.com/peterbourgon/ff/v3/ffcli"
	"github.com/peterbourgon/ff/v3/ffyaml"
)

// Build flags
var version = ""
var commit = ""
var date = ""

func main() {
	// Create a context that cancels on SIGINT
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Build the CLI command tree and run the command.
	cmd := newCommand()
	if err := cmd.ParseAndRun(ctx, os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func newCommand() *ffcli.Command {
	// Top-level flag set (common flags can go here)
	fs := flag.NewFlagSet("gdown", flag.ExitOnError)
	return &ffcli.Command{
		ShortUsage: "gdown [flags] <subcommand>",
		FlagSet:    fs,
		// No default Exec so that help is printed when no subcommand is provided.
		Exec: func(context.Context, []string) error {
			return flag.ErrHelp
		},
		Subcommands: []*ffcli.Command{
			newVersionCommand(),
			newDownloadCommand(),
			newCachedDownloadCommand(),
			newDownloadFolderCommand(),
			newExtractAllCommand(),
			newListFolderCommand(),
			newParseUrlCommand(),
		},
	}
}

func newVersionCommand() *ffcli.Command {
	return &ffcli.Command{
		Name:       "version",
		ShortUsage: "gdown version",
		ShortHelp:  "Print version information",
		Exec: func(ctx context.Context, args []string) error {
			v := version
			if v == "" {
				if buildInfo, ok := debug.ReadBuildInfo(); ok {
					v = buildInfo.Main.Version
				}
			}
			if v == "" {
				v = "dev"
			}
			fields := []string{v}
			if commit != "" {
				fields = append(fields, commit)
			}
			if date != "" {
				fields = append(fields, date)
			}
			fmt.Println(strings.Join(fields, " "))
			return nil
		},
	}
}

func newDownloadCommand() *ffcli.Command {
	cmd := "download"
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	urlFlag := fs.String("url", "", "URL of file to download (required)")
	output := fs.String("output", "", "Output file name (if empty, the basename of the URL is used)")
	quiet := fs.Bool("quiet", false, "Suppress logging")
	proxy := fs.String("proxy", "", "Proxy URL (e.g. http://host:port)")
	speed := fs.Int64("speed", 0, "Download speed limit in bytes/sec (0 means unlimited)")
	noCookies := fs.Bool("no-cookies", false, "Do not use cookies")
	noVerify := fs.Bool("no-verify", false, "Do not verify TLS certificate")
	resume := fs.Bool("resume", false, "Resume interrupted download")
	fuzzy := fs.Bool("fuzzy", false, "Fuzzy extraction of file ID (Google Drive only)")
	format := fs.String("format", "", "Format of Google Docs/Sheets/Slides (e.g. docx, xlsx, pptx)")
	userAgent := fs.String("user-agent", "", "User-Agent to use for downloading")
	return &ffcli.Command{
		Name:       cmd,
		ShortUsage: fmt.Sprintf("gdown %s [flags]", cmd),
		ShortHelp:  "Download a single file",
		FlagSet:    fs,
		Options: []ff.Option{
			ff.WithEnvVarPrefix("GDOWN"),
			ff.WithConfigFileFlag("config"),
			ff.WithConfigFileParser(ffyaml.Parser),
		},
		Exec: func(ctx context.Context, args []string) error {
			if *urlFlag == "" {
				return fmt.Errorf("flag -url is required")
			}
			opts := gdown.DownloadOptions{
				Quiet:      *quiet,
				Proxy:      *proxy,
				Speed:      *speed,
				UseCookies: !(*noCookies),
				Verify:     !(*noVerify),
				Resume:     *resume,
				Fuzzy:      *fuzzy,
				Format:     *format,
				UserAgent:  *userAgent,
			}
			result, err := gdown.Download(*urlFlag, *output, opts)
			if err != nil {
				return err
			}
			fmt.Printf("Downloaded file saved to: %s\n", result)
			return nil
		},
	}
}

func newCachedDownloadCommand() *ffcli.Command {
	cmd := "cachedownload"
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	urlFlag := fs.String("url", "", "URL of file to download (required)")
	output := fs.String("output", "", "Output file name/path (if empty, a cache file is used)")
	hash := fs.String("hash", "", "Expected hash in the format <algo>:<hash_value>")
	quiet := fs.Bool("quiet", false, "Suppress logging")
	proxy := fs.String("proxy", "", "Proxy URL")
	speed := fs.Int64("speed", 0, "Download speed limit (bytes/sec)")
	noCookies := fs.Bool("no-cookies", false, "Do not use cookies")
	noVerify := fs.Bool("no-verify", false, "Do not verify TLS certificate")
	resume := fs.Bool("resume", false, "Resume interrupted download")
	userAgent := fs.String("user-agent", "", "User-Agent to use")
	return &ffcli.Command{
		Name:       cmd,
		ShortUsage: fmt.Sprintf("gdown %s [flags]", cmd),
		ShortHelp:  "Download a file using caching",
		FlagSet:    fs,
		Options: []ff.Option{
			ff.WithEnvVarPrefix("GDOWN"),
			ff.WithConfigFileFlag("config"),
			ff.WithConfigFileParser(ffyaml.Parser),
		},
		Exec: func(ctx context.Context, args []string) error {
			if *urlFlag == "" {
				return fmt.Errorf("flag -url is required")
			}
			opts := gdown.DownloadOptions{
				Quiet:      *quiet,
				Proxy:      *proxy,
				Speed:      *speed,
				UseCookies: !(*noCookies),
				Verify:     !(*noVerify),
				Resume:     *resume,
				UserAgent:  *userAgent,
			}
			result, err := gdown.CachedDownload(*urlFlag, *output, *hash, *quiet, nil, opts)
			if err != nil {
				return err
			}
			fmt.Printf("Cached download complete. File saved to: %s\n", result)
			return nil
		},
	}
}

func newDownloadFolderCommand() *ffcli.Command {
	cmd := "downloadfolder"
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	// Either a URL or folder ID must be provided.
	urlFlag := fs.String("url", "", "Folder URL (if empty, use -id)")
	id := fs.String("id", "", "Folder ID (if -url is empty)")
	output := fs.String("output", "", "Output directory")
	quiet := fs.Bool("quiet", false, "Suppress logging")
	proxy := fs.String("proxy", "", "Proxy URL")
	speed := fs.Int64("speed", 0, "Download speed limit (bytes/sec)")
	noCookies := fs.Bool("no-cookies", false, "Do not use cookies")
	noVerify := fs.Bool("no-verify", false, "Do not verify TLS certificate")
	resume := fs.Bool("resume", false, "Resume interrupted downloads")
	userAgent := fs.String("user-agent", "", "User-Agent to use")
	remainingOk := fs.Bool("remaining-ok", false, "Allow folder contents to reach maximum limit")
	return &ffcli.Command{
		Name:       cmd,
		ShortUsage: fmt.Sprintf("gdown %s [flags]", cmd),
		ShortHelp:  "Download an entire Google Drive folder",
		FlagSet:    fs,
		Options: []ff.Option{
			ff.WithEnvVarPrefix("GDOWN"),
			ff.WithConfigFileFlag("config"),
			ff.WithConfigFileParser(ffyaml.Parser),
		},
		Exec: func(ctx context.Context, args []string) error {
			if *urlFlag == "" && *id == "" {
				return fmt.Errorf("either -url or -id must be specified")
			}
			opts := gdown.FolderOptions{
				DownloadOptions: gdown.DownloadOptions{
					Quiet:      *quiet,
					Proxy:      *proxy,
					Speed:      *speed,
					UseCookies: !(*noCookies),
					Verify:     !(*noVerify),
					Resume:     *resume,
					UserAgent:  *userAgent,
				},
				RemainingOk: *remainingOk,
			}
			files, err := gdown.DownloadFolder(*urlFlag, *id, *output, opts)
			if err != nil {
				return err
			}
			fmt.Println("Downloaded files:")
			for _, f := range files {
				fmt.Println("  -", f)
			}
			return nil
		},
	}
}

func newExtractAllCommand() *ffcli.Command {
	cmd := "extractall"
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	archive := fs.String("archive", "", "Path to archive file to extract (required)")
	to := fs.String("to", "", "Destination directory (if empty, the archive's directory is used)")
	return &ffcli.Command{
		Name:       cmd,
		ShortUsage: fmt.Sprintf("gdown %s [flags]", cmd),
		ShortHelp:  "Extract an archive file (zip, tar, tar.gz, etc.)",
		FlagSet:    fs,
		Options: []ff.Option{
			ff.WithEnvVarPrefix("GDOWN"),
			ff.WithConfigFileFlag("config"),
			ff.WithConfigFileParser(ffyaml.Parser),
		},
		Exec: func(ctx context.Context, args []string) error {
			if *archive == "" {
				return fmt.Errorf("flag -archive is required")
			}
			files, err := gdown.ExtractAll(*archive, *to)
			if err != nil {
				return err
			}
			fmt.Println("Extracted files:")
			for _, f := range files {
				fmt.Println("  -", f)
			}
			return nil
		},
	}
}

func newListFolderCommand() *ffcli.Command {
	cmd := "listfolder"
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	urlFlag := fs.String("url", "", "Folder URL (if empty, use -id)")
	id := fs.String("id", "", "Folder ID (if -url is empty)")
	quiet := fs.Bool("quiet", false, "Suppress logging")
	proxy := fs.String("proxy", "", "Proxy URL")
	speed := fs.Int64("speed", 0, "Download speed limit (bytes/sec)")
	noCookies := fs.Bool("no-cookies", false, "Do not use cookies")
	noVerify := fs.Bool("no-verify", false, "Do not verify TLS certificate")
	resume := fs.Bool("resume", false, "Resume downloads")
	userAgent := fs.String("user-agent", "", "User-Agent to use")
	remainingOk := fs.Bool("remaining-ok", false, "Allow folder contents to reach maximum limit")
	return &ffcli.Command{
		Name:       cmd,
		ShortUsage: fmt.Sprintf("gdown %s [flags]", cmd),
		ShortHelp:  "List contents of a Google Drive folder",
		FlagSet:    fs,
		Options: []ff.Option{
			ff.WithEnvVarPrefix("GDOWN"),
			ff.WithConfigFileFlag("config"),
			ff.WithConfigFileParser(ffyaml.Parser),
		},
		Exec: func(ctx context.Context, args []string) error {
			if *urlFlag == "" && *id == "" {
				return fmt.Errorf("either -url or -id must be specified")
			}
			opts := gdown.FolderOptions{
				DownloadOptions: gdown.DownloadOptions{
					Quiet:      *quiet,
					Proxy:      *proxy,
					Speed:      *speed,
					UseCookies: !(*noCookies),
					Verify:     !(*noVerify),
					Resume:     *resume,
					UserAgent:  *userAgent,
				},
				RemainingOk: *remainingOk,
			}
			infos, err := gdown.ListFolder(*urlFlag, *id, opts)
			if err != nil {
				return err
			}
			fmt.Println("Folder contents:")
			for _, info := range infos {
				if info.IsFolder {
					fmt.Printf("  Folder: %s\n", info.Path)
				} else {
					fmt.Printf("  File: %s\n    Download URL: %s\n", info.Path, info.DownloadURL)
				}
			}
			return nil
		},
	}
}

func newParseUrlCommand() *ffcli.Command {
	cmd := "parseurl"
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	urlFlag := fs.String("url", "", "URL to parse (required)")
	warn := fs.Bool("warn", true, "Emit warnings if the URL is not a download link")
	return &ffcli.Command{
		Name:       cmd,
		ShortUsage: fmt.Sprintf("gdown %s [flags]", cmd),
		ShortHelp:  "Parse a URL and extract a Google Drive file ID",
		FlagSet:    fs,
		Options: []ff.Option{
			ff.WithEnvVarPrefix("GDOWN"),
			ff.WithConfigFileFlag("config"),
			ff.WithConfigFileParser(ffyaml.Parser),
		},
		Exec: func(ctx context.Context, args []string) error {
			if *urlFlag == "" {
				return fmt.Errorf("flag -url is required")
			}
			fileId, isDownloadLink, err := gdown.ParseUrl(*urlFlag, *warn)
			if err != nil {
				return err
			}
			fmt.Printf("File ID: %s\nIs Download Link: %v\n", fileId, isDownloadLink)
			return nil
		},
	}
}
