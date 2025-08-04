# Image Downloader API (Go Backend)

![Go](https://img.shields.io/badge/Go-1.21+-blue)
![License](https://img.shields.io/badge/License-MIT-green)

A high-performance backend service for downloading multiple images and returning them as a ZIP archive.

## Features

- Download multiple images concurrently
- Handle special URLs (Next.js/Vercel optimized images)
- Automatic filename generation
- Clean temporary files after processing
- CORS support
- Health check endpoint

## API Endpoints

### `POST /download`

Download multiple images as ZIP:

```json
{
  "imageURLs": ["https://example.com/image1.jpg", "https://example.com/image2.png"],
  "destDir": "optional_custom_directory"
}
