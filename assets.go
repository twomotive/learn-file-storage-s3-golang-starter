package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
)

func (cfg apiConfig) ensureAssetsDir() error {
	if _, err := os.Stat(cfg.assetsRoot); os.IsNotExist(err) {
		return os.Mkdir(cfg.assetsRoot, 0755)
	}
	return nil
}

// ffprobeOutput defines the structure for the JSON output from ffprobe.
// We only care about the streams array and specific fields within it.
type ffprobeOutput struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	} `json:"streams"`
}

// getVideoAspectRatio runs ffprobe on the given file path to determine its aspect ratio.
func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("ffprobe failed: %w", err)
	}

	var probeData ffprobeOutput
	err = json.Unmarshal(out.Bytes(), &probeData)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal ffprobe output: %w", err)
	}

	for _, stream := range probeData.Streams {
		if stream.CodecType == "video" && stream.Width > 0 && stream.Height > 0 {
			ratio := float64(stream.Width) / float64(stream.Height)
			epsilon := 0.01 // Tolerance for floating point comparison

			if math.Abs(ratio-(16.0/9.0)) < epsilon {
				return "landscape", nil
			}
			if math.Abs(ratio-(9.0/16.0)) < epsilon {
				return "portrait", nil
			}
			return "other", nil
		}
	}

	return "", fmt.Errorf("no video stream found or invalid dimensions")
}

func processVideoFastStart(filePath string) (string, error) {
	outputPath := filePath + "." + "processing"
	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputPath)
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("ffmpeg failed: %w", err)
	}

	return outputPath, nil

}
