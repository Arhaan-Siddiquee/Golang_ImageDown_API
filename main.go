package main

import (
	"archive/zip"
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
	"time"

	"github.com/rs/cors"
)

func downloadImage(url string, filePath string, wg *sync.WaitGroup, errChan chan<- error) {
	defer wg.Done()

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

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

	_, err = io.Copy(file, resp.Body)
	if err != nil {
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

func cleanOldFiles(dir string) {
	files, _ := os.ReadDir(dir)
	now := time.Now()
	for _, file := range files {
		info, _ := file.Info()
		if now.Sub(info.ModTime()) > 24*time.Hour {
			os.Remove(filepath.Join(dir, file.Name()))
		}
	}
}

func downloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request struct {
		URLs      []string `json:"urls"`
		ZipReturn bool     `json:"zipReturn"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(request.URLs) == 0 {
		http.Error(w, "No URLs provided", http.StatusBadRequest)
		return
	}

	if len(request.URLs) > 10 {
		http.Error(w, "Maximum 10 URLs allowed", http.StatusBadRequest)
		return
	}

	destDir := "downloaded_images"
	if err := os.MkdirAll(destDir, 0755); err != nil {
		http.Error(w, "Failed to create directory", http.StatusInternalServerError)
		return
	}

	if time.Now().Hour() == 0 { 
		cleanOldFiles(destDir)
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(request.URLs))
	downloadedFiles := make([]string, 0, len(request.URLs))

	for _, originalURL := range request.URLs {
		wg.Add(1)
		fileName := generateFilename(originalURL)
		filePath := filepath.Join(destDir, fileName)
		downloadedFiles = append(downloadedFiles, filePath)
		go downloadImage(originalURL, filePath, &wg, errChan)
	}

	wg.Wait()
	close(errChan)

	hasErrors := false
	for err := range errChan {
		fmt.Println("Download error:", err)
		hasErrors = true
	}

	if request.ZipReturn {
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

			_, err = io.Copy(entry, file)
			file.Close()
			if err != nil {
				continue
			}
		}
	} else {
		response := struct {
			Success    bool     `json:"success"`
			SavedPaths []string `json:"savedPaths"`
			ErrorCount int      `json:"errorCount"`
		}{
			Success:    !hasErrors,
			SavedPaths: downloadedFiles,
			ErrorCount: len(request.URLs) - len(downloadedFiles),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

func main() {
	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:  []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:  []string{"Content-Type"},
		MaxAge:          300,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/download", downloadHandler)

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("Server started on :%s\n", port)
	http.ListenAndServe(":"+port, c.Handler(mux))
}