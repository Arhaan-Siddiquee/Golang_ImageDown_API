package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

func downloadImage(url string, filePath string, wg *sync.WaitGroup, errChan chan<- error) {
	defer wg.Done()

	fmt.Printf("Starting download for: %s\n", url)
		
	file, err := os.Create(filePath)
	if err != nil {
		errChan <- fmt.Errorf("failed to create file %s: %w", filePath, err)
		return
	}
	defer file.Close()

	resp, err := http.Get(url)
	if err != nil {
		errChan <- fmt.Errorf("failed to fetch URL %s: %w", url, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errChan <- fmt.Errorf("bad status code for %s: %d", url, resp.StatusCode)
		return
	}

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		errChan <- fmt.Errorf("failed to write image to file %s: %w", filePath, err)
		return
	}

	fmt.Printf("Successfully downloaded: %s to %s\n", url, filePath)
}

func DownloadImagesConcurrently(imageURLs []string, destDir string) {
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		fmt.Printf("Creating directory: %s\n", destDir)
		err = os.MkdirAll(destDir, 0755)
		if err != nil {
			fmt.Printf("Error creating directory %s: %v\n", destDir, err)
			return
		}
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(imageURLs))

	fmt.Printf("Starting concurrent image downloads to %s...\n", destDir)
	startTime := time.Now()

	for _, originalURL := range imageURLs {
		wg.Add(1)

		parsedURL, err := url.Parse(originalURL)
		if err != nil {
			errChan <- fmt.Errorf("failed to parse URL %s: %w", originalURL, err)
			wg.Done()
			continue
		}

		fileName := ""
		if (parsedURL.Host == "nextjs.org" && strings.HasPrefix(parsedURL.Path, "/_next/image")) ||
		   (parsedURL.Host == "vercel-storage.com" && strings.Contains(parsedURL.Path, "/_next/image")) {
			rawImageURL := parsedURL.Query().Get("url")
			if rawImageURL != "" {
				decodedImageURL, decodeErr := url.QueryUnescape(rawImageURL)
				if decodeErr == nil {
					fileName = filepath.Base(decodedImageURL)
				}
			}
		}

		if fileName == "" || fileName == "." || fileName == "/" {
			fileName = filepath.Base(parsedURL.Path)
			if fileName == "." || fileName == "/" || fileName == "" {
				hash := sha256.Sum256([]byte(originalURL))
				fileName = fmt.Sprintf("image_%x", hash[:8])
			}
		}

		if filepath.Ext(fileName) == "" {
			pathExt := filepath.Ext(parsedURL.Path)
			if pathExt != "" && (pathExt == ".jpg" || pathExt == ".jpeg" || pathExt == ".png" || pathExt == ".gif" || pathExt == ".webp") {
				fileName += pathExt
			} else {
				fileName += ".jpg"
			}
		}

		reg := regexp.MustCompile(`[^a-zA-Z0-9\.\-_]`)
		fileName = reg.ReplaceAllString(fileName, "_")

		filePath := filepath.Join(destDir, fileName)

		go downloadImage(originalURL, filePath, &wg, errChan)
	}

	wg.Wait()
	close(errChan)

	hasErrors := false
	for err := range errChan {
		fmt.Printf("Download error: %v\n", err)
		hasErrors = true
	}

	if hasErrors {
		fmt.Println("Some images failed to download.")
	} else {
		fmt.Println("All images processed successfully (or no errors reported).")
	}

	fmt.Printf("All downloads completed in %v\n", time.Since(startTime))
}

func main() {
	exampleURLs := []string{
		"https://arhaan-dev.vercel.app/assets/me-BscloOhD.png",
	}
	destinationDirectory := "downloaded_images"

	DownloadImagesConcurrently(exampleURLs, destinationDirectory)
}
