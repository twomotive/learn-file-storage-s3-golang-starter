package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	maxSize := 1 << 30

	r.Body = http.MaxBytesReader(w, r.Body, int64(maxSize))

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

	metaDataVideo, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Video not found", err)
		return
	}

	if metaDataVideo.UserID != userID {
		respondWithError(w, http.StatusForbidden, "User does not own this video", nil)
		return
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to form file :", err)
		return
	}
	defer file.Close()

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
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid file type. Only mp4 are allowed.", nil)
		return
	}

	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error while creating temp file ", err)
		return
	}
	defer tempFile.Close()
	defer os.Remove(tempFile.Name())

	io.Copy(tempFile, file)

	// Close the temp file so ffmpeg can process it
	tempFile.Close()

	// Process the video to ensure 'faststart'
	processedFilePath, err := processVideoFastStart(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to process video", err)
		return
	}
	// Ensure the processed file is cleaned up
	defer os.Remove(processedFilePath)

	// Open the processed file for reading
	processedFile, err := os.Open(processedFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to open processed video file", err)
		return
	}
	defer processedFile.Close()

	randomBytes := make([]byte, 32)
	_, err = rand.Read(randomBytes)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to generate random filekey", err)
		return
	}
	// Encode random bytes to base64 string
	randomString := base64.RawURLEncoding.EncodeToString(randomBytes)
	// Get aspect ratio from the *processed* file
	prefix, err := getVideoAspectRatio(processedFilePath)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "cannot get video prefix", err)
		return
	}

	fileKey := fmt.Sprintf("%s/%s", prefix, randomString)

	putInput := &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &fileKey,
		Body:        processedFile,
		ContentType: &mediaType,
	}

	_, err = cfg.s3Client.PutObject(r.Context(), putInput)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to upload video to S3", err)
		return
	}

	// Store CloudFront URL instead of bucket and key
	videoLocation := fmt.Sprintf("https://%s/%s", cfg.s3CfDistribution, fileKey)
	metaDataVideo.VideoURL = &videoLocation

	err = cfg.db.UpdateVideo(metaDataVideo)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to update video metadata in database", err)
		return
	}

	respondWithJSON(w, http.StatusOK, struct{}{})

}
