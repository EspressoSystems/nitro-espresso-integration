package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

const targetDir = "../target"

const baseURL = "https://github.com/EspressoSystems/espresso-network-go/releases"

func main() {
	var version string
	var url string

	var rootCmd = &cobra.Command{Use: "app"}
	var downloadCmd = &cobra.Command{
		Use:   "download",
		Short: "Download the static library",
		Run: func(cmd *cobra.Command, args []string) {
			download(version, url)
		},
	}
	downloadCmd.Flags().StringVarP(&version, "version", "v", "latest", "Specify the version to download")
	downloadCmd.Flags().StringVarP(&url, "url", "u", "", "Specify the url to download. If this is set, the version flag will be ignored")

	var cleanCmd = &cobra.Command{
		Use:   "clean",
		Short: "Clean the downloaded files",
		Run: func(cmd *cobra.Command, args []string) {
			clean()
		},
	}

	rootCmd.AddCommand(downloadCmd, cleanCmd)
	err := rootCmd.Execute()
	if err != nil {
		fmt.Printf("Failed to execute command: %s\n", err)
		os.Exit(1)
	}
}

func download(version string, specifiedUrl string) {
	fileName := getFileName()
	fileDir := getFileDir()
	libFilePath := filepath.Join(fileDir, fileName)

	if _, err := os.Stat(libFilePath); err == nil {
		fmt.Println("File already exists. Run clean to remove it first.")
		return
	}

	if err := os.MkdirAll(fileDir, 0755); err != nil {
		fmt.Printf("Failed to create target directory: %s\n", err)
		os.Exit(1)
	}

	var url string
	if specifiedUrl != "" {
		fmt.Printf("Using specified url to download the library: %s\n", specifiedUrl)
		url = specifiedUrl
	} else {
		url = fmt.Sprintf("%s/download/%s/%s", baseURL, version, fileName)
	}

	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("Failed to download static library: %s\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	out, err := os.Create(libFilePath)
	if err != nil {
		fmt.Printf("Failed to create file: %s\n", err)
		os.Exit(1)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		fmt.Printf("Failed to write file: %s\n", err)
		os.Exit(1)
	}

	fmt.Printf("Static library downloaded to: %s\n", libFilePath)
}

func clean() {
	fileDir := getFileDir()
	err := os.RemoveAll(fileDir)
	if err != nil {
		fmt.Printf("Failed to clean files: %s\n", err)
		os.Exit(1)
	}
	fmt.Println("Cleaned downloaded files.")
}

func getFileName() string {
	arch := runtime.GOARCH
	os := runtime.GOOS

	var fileName string
	var extension string

	// Determine file extension based on OS
	if os == "darwin" {
		extension = ".dylib"
	} else if os == "linux" {
		extension = ".so"
	} else {
		panic(fmt.Sprintf("unsupported OS: %s", os))
	}

	// Determine architecture-specific prefix
	switch arch {
	case "amd64":
		if os == "darwin" {
			fileName = "x86_64-apple-darwin"
		} else if os == "linux" {
			fileName = "x86_64-unknown-linux-musl"
		}
	case "arm64":
		if os == "darwin" {
			fileName = "aarch64-apple-darwin"
		} else if os == "linux" {
			fileName = "aarch64-unknown-linux-musl"
		}
	default:
		panic(fmt.Sprintf("unsupported architecture: %s", arch))
	}

	return fmt.Sprintf("libespresso_crypto_helper-%s%s", fileName, extension)
}

func getFileDir() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		panic("No caller information")
	}

	return filepath.Join(path.Dir(filename), targetDir)
}
