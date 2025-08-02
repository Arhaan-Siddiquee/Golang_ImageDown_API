package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/rs/cors"
)

func main() {
	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:          300,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/", rootHandler)
	mux.HandleFunc("/download", downloadHandler)
	mux.HandleFunc("/health", healthHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server started on :%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, c.Handler(mux)))
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "active",
		"message": "Image Download API - POST to /download",
	})
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "OK"})
}

func downloadImage(url string, filePath string, wg *sync.WaitGroup, errChan chan<- error) {
	defer wg.Done()
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		errChan <- fmt.Errorf("failed to fetch URL %s: %v", url, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errChan <- fmt.Errorf("bad status code for %s: %d", url, resp.StatusCode)
		return
	}

	file, err := os.Create(filePath)
	if err != nil {
		errChan <- fmt.Errorf("failed to create file %s: %v", filePath, err)
		return
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		errChan <- fmt.Errorf("failed to write image to file %s: %v", filePath, err)
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
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request struct {
		ImageURLs []string `json:"imageURLs"`
		DestDir   string   `json:"destDir"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(request.ImageURLs) == 0 {
		http.Error(w, "No URLs provided", http.StatusBadRequest)
		return
	}

	destDir := "temp_downloads"
	if err := os.MkdirAll(destDir, 0755); err != nil {
		http.Error(w, "Failed to create directory", http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(destDir)

	var wg sync.WaitGroup
	errChan := make(chan error, len(request.ImageURLs))
	downloadedFiles := make([]string, 0, len(request.ImageURLs))

	for _, url := range request.ImageURLs {
		wg.Add(1)
		fileName := generateFilename(url)
		filePath := filepath.Join(destDir, fileName)
		downloadedFiles = append(downloadedFiles, filePath)
		go downloadImage(url, filePath, &wg, errChan)
	}

	wg.Wait()
	close(errChan)

	errorCount := 0
	for err := range errChan {
		log.Println("Download error:", err)
		errorCount++
	}

	if len(downloadedFiles) == 0 {
		http.Error(w, "No files were downloaded", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=images.zip")

	zipWriter := zip.NewWriter(w)
	defer zipWriter.Close()

	for _, filePath := range downloadedFiles {
		file, err := os.Open(filePath)
		if err != nil {
			continue
		}

		entry, err := zipWriter.Create(filepath.Base(filePath))
		if err != nil {
			file.Close()
			continue
		}

		if _, err := io.Copy(entry, file); err != nil {
			file.Close()
			continue
		}
		file.Close()
	}
}