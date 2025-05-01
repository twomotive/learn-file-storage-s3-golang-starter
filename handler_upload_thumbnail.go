package main

import (
	"crypto/rand"     // Added import for crypto/rand
	"encoding/base64" // Added import for base64 encoding
	"fmt"
	"io"
	"mime" // Added import for mime types
	"net/http"
	"os"            // Added import for os operations
	"path/filepath" // Added import for file path operations

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	maxMemory := 10 << 20

	r.ParseMultipartForm(int64(maxMemory))

	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to form file :", err)
		return
	}
	defer file.Close()

	metaDataVideo, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Video not found", err)
		return
	}

	if metaDataVideo.UserID != userID {
		respondWithError(w, http.StatusForbidden, "User does not own this video", nil)
		return
	}

	mediaType := header.Header.Get("Content-Type")
	if mediaType == "" {
		respondWithError(w, http.StatusBadRequest, "Could not determine file media type", nil)
		return
	}

	// Parse the media type
	mediaType, _, err = mime.ParseMediaType(mediaType)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Could not parse media type", err)
		return
	}

	// Validate the media type
	if mediaType != "image/jpeg" && mediaType != "image/png" {
		respondWithError(w, http.StatusBadRequest, "Invalid file type. Only JPEG and PNG images are allowed.", nil)
		return
	}

	// Determine file extension from media type
	extensions, err := mime.ExtensionsByType(mediaType)
	if err != nil || len(extensions) == 0 {
		respondWithError(w, http.StatusBadRequest, "Could not determine file extension from media type", err)
		return
	}
	fileExtension := extensions[0] // Use the first suggested extension

	// Generate random bytes for the filename
	randomBytes := make([]byte, 32)
	_, err = rand.Read(randomBytes)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to generate random filename", err)
		return
	}
	// Encode random bytes to base64 string
	randomString := base64.RawURLEncoding.EncodeToString(randomBytes)

	// Construct the file path using the random string and extension
	fileName := fmt.Sprintf("%s%s", randomString, fileExtension)
	filePath := filepath.Join(cfg.assetsRoot, fileName)

	// Create the destination file
	dst, err := os.Create(filePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to create file on disk", err)
		return
	}
	defer dst.Close()

	// Copy the uploaded file data to the destination file
	_, err = io.Copy(dst, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to copy file data", err)
		// Attempt to remove the partially created file
		os.Remove(filePath)
		return
	}

	// Construct the URL for the saved thumbnail
	// Assumes the server is running on localhost and uses the configured port
	thumbnailURL := fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port, fileName)
	metaDataVideo.ThumbnailURL = &thumbnailURL

	err = cfg.db.UpdateVideo(metaDataVideo)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to update video metadata", err)
		// Attempt to remove the saved file if DB update fails
		os.Remove(filePath)
		return
	}

	respondWithJSON(w, http.StatusOK, struct{}{})
}
