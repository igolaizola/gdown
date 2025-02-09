# gdown

**gdown** is a Go package and CLI tool for downloading files and folders from Google Drive.
It is a complete re‑implementation of [wkentaro/gdown](https://github.com/wkentaro/gdown) (originally written in Python) in Golang.

This project was created by asking **chatgpt o3-mini-high** to convert the original Python code into Go.
Even this README was written by **chatgpt o3-mini-high**.

## 🚀 Features

- **Download Files:** Download files from Google Drive via URL or file ID.
- **Cached Downloads:** Use a caching mechanism to avoid repeated downloads.
- **Resume Downloads:** Resume interrupted downloads.
- **Download Folders:** Recursively download an entire Google Drive folder with preserved structure.
- **List Folder Contents:** Retrieve detailed information about the files and folders within a Google Drive folder, including individual download URLs.
- **Extract Archives:** Extract archive files (e.g., ZIP, TAR, TAR.GZ) to a specified directory.
- **CLI Interface:** The project provides a comprehensive CLI with subcommands for each public function, powered by [ffcli](https://github.com/peterbourgon/ff).

## 📦 Installation

### Download binary

Download the latest release binary from the [Releases](https://github.com/igolaizola/gdown/releases).

### Using `go install`

Ensure you have [Go](https://golang.org/dl/) installed. Then install the package with:

```bash
go install github.com/igolaizola/gdown/cmd/gdown@latest
```

### 🛠️ Build from Source

Clone the repository and build the CLI tool:

```bash
git clone https://github.com/igolaizola/gdown.git
cd gdown
go build -o gdown cmd/gdown/main.go
```

## 📋 Usage

### Command-Line Interface (CLI)

The CLI tool exposes subcommands for each public function in the package.

#### 📝 Version

Print the version information:

```bash
./gdown version
```

#### 📥 Download a File

Download a single file from Google Drive by URL:

```bash
./gdown download -url "https://drive.google.com/uc?id=FILE_ID" -output "myfile.txt"
```

Flags:

- `-url`: URL of the file to download (required).
- `-output`: Output file name (if empty, the basename of the URL is used).
- `-quiet`: Suppress logging output.
- `-proxy`: Set a proxy URL (e.g., `http://host:port`).
- `-speed`: Limit download speed in bytes per second (0 means unlimited).
- `-no-cookies`: Do not use cookies.
- `-no-verify`: Skip TLS certificate verification.
- `-resume`: Resume an interrupted download.
- `-fuzzy`: Enable fuzzy file ID extraction (Google Drive only).
- `-format`: Specify a file format (for Google Docs/Sheets/Slides).
- `-user-agent`: Custom User-Agent string.

#### 🗃️ Cached Download

Download a file using a caching mechanism:

```bash
./gdown cachedownload -url "https://drive.google.com/uc?id=FILE_ID" -output "cachedfile.txt" -hash "md5:YOUR_HASH"
```

#### 📂 Download a Folder

Download an entire Google Drive folder:

```bash
./gdown downloadfolder -id "FOLDER_ID" -output "folder_download_dir"
```

Flags:

- `-url` or `-id`: Provide either the folder URL or the folder ID.
- Other flags are similar to the file download options.

#### 📑 List Folder Contents

List the contents of a Google Drive folder, showing details for each file (including a download URL):

```bash
./gdown listfolder -id "FOLDER_ID"
```

#### 📦 Extract an Archive

Extract an archive file (ZIP, TAR, TAR.GZ, etc.):

```bash
./gdown extractall -archive "archive.zip" -to "destination_dir"
```

#### 🔍 Parse a URL

Extract a Google Drive file ID from a URL:

```bash
./gdown parseurl -url "https://drive.google.com/file/d/FILE_ID/view"
```

### 🧑‍💻 Programmatic Usage

You can also use **gdown** as a library in your own Go projects. For example:

```go
package main

import (
    "fmt"
    "log"

    "github.com/igolaizola/gdown"
)

func main() {
    opts := gdown.DownloadOptions{
        Quiet:      false,
        UseCookies: true,
        Verify:     true,
        Resume:     true,
        Speed:      0, // unlimited
    }
    output, err := gdown.Download("https://drive.google.com/uc?id=FILE_ID", "myfile.txt", opts)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Downloaded file saved to: %s\n", output)
}
```

## 🏗️ Project Background & Credits

- **Based on gdown:** This project is inspired by and based on [gdown](https://github.com/wkentaro/gdown), a popular Python tool for downloading files from Google Drive.
- **Conversion to Go:** The original Python code was converted into Golang by asking **chatgpt o3-mini-high** to help translate and adapt the logic.
- **Contributors:**
  - Original gdown: [wkentaro](https://github.com/wkentaro)
  - gdown conversion: **chatgpt o3-mini-high** (with human oversight)

## 📚 Dependencies

- [goquery](https://github.com/PuerkitoBio/goquery) for HTML parsing.
- [ffcli](https://github.com/peterbourgon/ff) for CLI flag parsing and subcommand handling.
- [ffyaml](https://github.com/peterbourgon/ff) for YAML configuration file support.

Install dependencies via:

```bash
go get github.com/PuerkitoBio/goquery
go get github.com/peterbourgon/ff/v3
go get github.com/peterbourgon/ff/v3/ffyaml
```

## 📜 License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
