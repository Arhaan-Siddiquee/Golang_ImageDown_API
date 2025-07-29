package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

func downloadImage(url string, filePath string, wg *sync.WaitGroup, errChan chan<- error) {
	defer wg.Done()

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
}

func generateFilename(originalURL string) string {
	parsedURL, err := url.Parse(originalURL)
	if err != nil {
		hash := sha256.Sum256([]byte(originalURL))
		return fmt.Sprintf("image_%x.jpg", hash[:8])
	}

	fileName := ""
	if (parsedURL.Host == "nextjs.org" && strings.HasPrefix(parsedURL.Path, "/_next/image")) ||
		(parsedURL.Host == "vercel-storage.com" && strings.Contains(parsedURL.Path, "/_next/image")) {
		if rawImageURL := parsedURL.Query().Get("url"); rawImageURL != "" {
			if decodedImageURL, err := url.QueryUnescape(rawImageURL); err == nil {
				fileName = filepath.Base(decodedImageURL)
			}
		}
	}

	if fileName == "" {
		fileName = filepath.Base(parsedURL.Path)
		if fileName == "." || fileName == "/" {
			hash := sha256.Sum256([]byte(originalURL))
			fileName = fmt.Sprintf("image_%x", hash[:8])
		}
	}

	if filepath.Ext(fileName) == "" {
		pathExt := filepath.Ext(parsedURL.Path)
		if pathExt != "" {
			fileName += pathExt
		} else {
			fileName += ".jpg"
		}
	}

	reg := regexp.MustCompile(`[^a-zA-Z0-9\.\-_]`)
	return reg.ReplaceAllString(fileName, "_")
}

func downloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request struct {
		URLs []string `json:"urls"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	destDir := "downloaded_images"
	if err := os.MkdirAll(destDir, 0755); err != nil {
		http.Error(w, "Failed to create directory", http.StatusInternalServerError)
		return
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(request.URLs))
	results := make([]string, 0, len(request.URLs))

	for _, originalURL := range request.URLs {
		wg.Add(1)
		fileName := generateFilename(originalURL)
		filePath := filepath.Join(destDir, fileName)
		results = append(results, filePath)
		go downloadImage(originalURL, filePath, &wg, errChan)
	}

	wg.Wait()
	close(errChan)

	hasErrors := false
	for err := range errChan {
		fmt.Println("Download error:", err)
		hasErrors = true
	}

	response := struct {
		Success   bool     `json:"success"`
		SavedTo   []string `json:"saved_to"`
		ErrorCount int     `json:"error_count"`
	}{
		Success:   !hasErrors,
		SavedTo:   results,
		ErrorCount: len(request.URLs) - len(results),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func main() {
	http.HandleFunc("/download", downloadHandler)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Printf("Server started on :%s\n", port)
	http.ListenAndServe(":"+port, nil)
}