package downloader

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"YoutubeDownloader/internal/utils"
)

var downloadPercentPattern = regexp.MustCompile(`\[download\]\s+(\d+(?:\.\d+)?)%`)

// progressOutput сохраняет диагностику yt-dlp и передает процент из его вывода.
type progressOutput struct {
	mu       sync.Mutex
	output   bytes.Buffer
	line     bytes.Buffer
	lastStep int
	onUpdate func(int)
}

func (p *progressOutput) Write(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = p.output.Write(data)
	for _, ch := range data {
		if ch == '\n' || ch == '\r' {
			if match := downloadPercentPattern.FindSubmatch(p.line.Bytes()); len(match) == 2 {
				value, err := strconv.ParseFloat(string(match[1]), 64)
				if err == nil {
					percent := int(value)
					if percent > 99 {
						percent = 99 // Финальная обработка и отправка еще впереди.
					}
					step := percent / 5
					if step > p.lastStep && p.onUpdate != nil {
						p.lastStep = step
						p.onUpdate(percent)
					}
				}
			}
			p.line.Reset()
		} else {
			_ = p.line.WriteByte(ch)
		}
	}
	return len(data), nil
}

// DownloadYouTubeVideo скачивает видео с YouTube
func DownloadYouTubeVideo(url string) (string, error) {
	tmpDir := "./tmp"
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", errors.New("не удалось создать временную папку: " + err.Error())
	}

	// Очищаем старые временные файлы
	utils.CleanupTempFiles(tmpDir)

	// Диагностируем файловую систему
	if err := utils.DiagnoseFileSystem(tmpDir); err != nil {
		return "", fmt.Errorf("проблемы с файловой системой: %v", err)
	}

	filename := filepath.Join(tmpDir, "ytvideo_"+utils.RandomString(8)+".mp4")
	absFilename, _ := filepath.Abs(filename)

	var ytDlpPath string
	if runtime.GOOS == "windows" {
		ytDlpPath = "./yt-dlp.exe"
	} else {
		ytDlpPath = "./yt-dlp_linux"
	}
	absYtDlpPath, _ := filepath.Abs(ytDlpPath)

	// Пробуем разные стратегии скачивания
	strategies := []struct {
		name string
		args []string
	}{
		{
			name: "best_quality",
			args: []string{"-f", "bestvideo[ext=mp4]+bestaudio[ext=m4a]/best[ext=mp4]/best", "-o", absFilename, "--no-mtime", "--no-warnings"},
		},
		{
			name: "simple_best",
			args: []string{"-f", "best[ext=mp4]/best", "-o", absFilename, "--no-mtime", "--no-warnings"},
		},
		{
			name: "worst_quality",
			args: []string{"-f", "worst[ext=mp4]/worst", "-o", absFilename, "--no-mtime", "--no-warnings"},
		},
	}

	var lastError error
	for i, strategy := range strategies {
		fmt.Printf("[DOWNLOADER] Пробуем стратегию %d: %s\n", i+1, strategy.name)

		cmd := exec.Command(absYtDlpPath, strategy.args...)
		cmd.Args = append(cmd.Args, url)

		output, err := cmd.CombinedOutput()
		if err != nil {
			lastError = fmt.Errorf("yt-dlp error (strategy %s): %v, details: %s", strategy.name, err, string(output))
			fmt.Printf("[DOWNLOADER] Стратегия %s не удалась: %v\n", strategy.name, err)
			continue
		}

		// Проверяем, создался ли файл
		if _, err := os.Stat(absFilename); err == nil {
			fmt.Printf("[DOWNLOADER] Успешно скачано с помощью стратегии: %s\n", strategy.name)
			return absFilename, nil
		}

		// Ищем файл с другим расширением
		baseName := strings.TrimSuffix(absFilename, ".mp4")
		possibleExtensions := []string{".mp4", ".mkv", ".webm", ".avi", ".mov"}

		for _, ext := range possibleExtensions {
			altFilename := baseName + ext
			if _, err := os.Stat(altFilename); err == nil {
				fmt.Printf("[DOWNLOADER] Найден файл с расширением %s: %s\n", ext, altFilename)
				return altFilename, nil
			}
		}

		lastError = fmt.Errorf("файл не был создан после стратегии %s, yt-dlp output: %s", strategy.name, string(output))
	}

	return "", fmt.Errorf("все стратегии скачивания не удались. Последняя ошибка: %v", lastError)
}

// DownloadYouTubeVideoWithUserID скачивает видео с YouTube с уникальным идентификатором пользователя
func DownloadYouTubeVideoWithUserID(url string, userID int64, requestID string) (string, error) {
	return DownloadYouTubeVideoWithUserIDProgress(url, userID, requestID, nil)
}

// DownloadYouTubeVideoWithUserIDProgress сообщает процент загрузки из yt-dlp.
func DownloadYouTubeVideoWithUserIDProgress(url string, userID int64, requestID string, onProgress func(int)) (string, error) {
	return DownloadYouTubeVideoWithUserIDQualityProgress(url, userID, requestID, 0, onProgress, nil)
}

// DownloadYouTubeVideoWithUserIDQualityProgress selects a native resolution or downscales it.
func DownloadYouTubeVideoWithUserIDQualityProgress(url string, userID int64, requestID string, quality int, onProgress func(int), onConversion func()) (string, error) {
	var plan qualityPlan
	if quality < 0 {
		return "", ErrQualityUnavailable
	}
	if quality != 0 {
		metadata, err := probeVideo(url)
		if err != nil {
			return "", err
		}
		plan, err = planQuality(metadata, quality, ffmpegAvailable())
		if err != nil {
			return "", err
		}
	}
	tmpDir := "./tmp"
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", errors.New("не удалось создать временную папку: " + err.Error())
	}

	// Очищаем старые временные файлы
	utils.CleanupTempFiles(tmpDir)

	// Диагностируем файловую систему
	if err := utils.DiagnoseFileSystem(tmpDir); err != nil {
		return "", fmt.Errorf("проблемы с файловой системой: %v", err)
	}

	// Создаем уникальное имя файла с userID и requestID
	filename := filepath.Join(tmpDir, fmt.Sprintf("ytvideo_user%d_%s.mp4", userID, requestID))
	absFilename, _ := filepath.Abs(filename)

	var ytDlpPath string
	if runtime.GOOS == "windows" {
		ytDlpPath = "./yt-dlp.exe"
	} else {
		ytDlpPath = "./yt-dlp_linux"
	}
	absYtDlpPath, _ := filepath.Abs(ytDlpPath)

	// Пробуем разные стратегии скачивания
	strategies := []struct {
		name string
		args []string
	}{
		{
			name: "best_quality",
			args: []string{"-f", "bestvideo[ext=mp4]+bestaudio[ext=m4a]/best[ext=mp4]/best", "-o", absFilename, "--no-mtime", "--no-warnings", "--newline"},
		},
		{
			name: "simple_best",
			args: []string{"-f", "best[ext=mp4]/best", "-o", absFilename, "--no-mtime", "--no-warnings", "--newline"},
		},
		{
			name: "worst_quality",
			args: []string{"-f", "worst[ext=mp4]/worst", "-o", absFilename, "--no-mtime", "--no-warnings", "--newline"},
		},
	}
	if quality != 0 {
		strategies = []struct {
			name string
			args []string
		}{{name: "selected_quality", args: []string{"-f", plan.selector, "--merge-output-format", "mp4", "-o", absFilename, "--no-mtime", "--no-warnings", "--newline"}}}
	}

	var lastError error
	for i, strategy := range strategies {
		fmt.Printf("[DOWNLOADER] [%s] Пользователь %d: пробуем стратегию %d: %s\n", requestID, userID, i+1, strategy.name)

		cmd := exec.Command(absYtDlpPath, strategy.args...)
		cmd.Args = append(cmd.Args, "--ignore-config", "--no-playlist", "--", url)

		progress := &progressOutput{onUpdate: onProgress}
		cmd.Stdout = progress
		cmd.Stderr = progress
		err := cmd.Run()
		output := progress.output.Bytes()
		if err != nil {
			lastError = fmt.Errorf("yt-dlp error (strategy %s): %v, details: %s", strategy.name, err, string(output))
			fmt.Printf("[DOWNLOADER] [%s] Пользователь %d: стратегия %s не удалась: %v\n", requestID, userID, strategy.name, err)
			continue
		}

		// Проверяем, создался ли файл
		if _, err := os.Stat(absFilename); err == nil {
			fmt.Printf("[DOWNLOADER] [%s] Пользователь %d: успешно скачано с помощью стратегии: %s\n", requestID, userID, strategy.name)
			if plan.convert {
				if onConversion != nil {
					onConversion()
				}
				return convertVideo(absFilename, quality)
			}
			return absFilename, nil
		}

		// Ищем файл с другим расширением
		baseName := strings.TrimSuffix(absFilename, ".mp4")
		possibleExtensions := []string{".mp4", ".mkv", ".webm", ".avi", ".mov"}

		for _, ext := range possibleExtensions {
			altFilename := baseName + ext
			if _, err := os.Stat(altFilename); err == nil {
				fmt.Printf("[DOWNLOADER] [%s] Пользователь %d: найден файл с расширением %s: %s\n", requestID, userID, ext, altFilename)
				if plan.convert {
					if onConversion != nil {
						onConversion()
					}
					return convertVideo(altFilename, quality)
				}
				return altFilename, nil
			}
		}

		lastError = fmt.Errorf("файл не был создан после стратегии %s, yt-dlp output: %s", strategy.name, string(output))
	}

	return "", fmt.Errorf("все стратегии скачивания не удались для пользователя %d (request %s). Последняя ошибка: %v", userID, requestID, lastError)
}

// DownloadYouTubeVideoWithUserIDAndURL скачивает видео с YouTube с уникальным идентификатором пользователя и URL хешем
func DownloadYouTubeVideoWithUserIDAndURL(url string, userID int64, requestID string, urlHash string) (string, error) {
	tmpDir := "./tmp"
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", errors.New("не удалось создать временную папку: " + err.Error())
	}

	// Очищаем старые временные файлы
	utils.CleanupTempFiles(tmpDir)

	// Диагностируем файловую систему
	if err := utils.DiagnoseFileSystem(tmpDir); err != nil {
		return "", fmt.Errorf("проблемы с файловой системой: %v", err)
	}

	// Создаем уникальное имя файла с userID, requestID и urlHash
	filename := filepath.Join(tmpDir, fmt.Sprintf("ytvideo_user%d_%s_%s.mp4", userID, requestID, urlHash))
	absFilename, _ := filepath.Abs(filename)

	var ytDlpPath string
	if runtime.GOOS == "windows" {
		ytDlpPath = "./yt-dlp.exe"
	} else {
		ytDlpPath = "./yt-dlp_linux"
	}
	absYtDlpPath, _ := filepath.Abs(ytDlpPath)

	// Пробуем разные стратегии скачивания
	strategies := []struct {
		name string
		args []string
	}{
		{
			name: "best_quality",
			args: []string{"-f", "bestvideo[ext=mp4]+bestaudio[ext=m4a]/best[ext=mp4]/best", "-o", absFilename, "--no-mtime", "--no-warnings"},
		},
		{
			name: "simple_best",
			args: []string{"-f", "best[ext=mp4]/best", "-o", absFilename, "--no-mtime", "--no-warnings"},
		},
		{
			name: "worst_quality",
			args: []string{"-f", "worst[ext=mp4]/worst", "-o", absFilename, "--no-mtime", "--no-warnings"},
		},
	}

	var lastError error
	for i, strategy := range strategies {
		fmt.Printf("[DOWNLOADER] [%s] Пользователь %d (URL: %s): пробуем стратегию %d: %s\n", requestID, userID, urlHash, i+1, strategy.name)

		cmd := exec.Command(absYtDlpPath, strategy.args...)
		cmd.Args = append(cmd.Args, url)

		output, err := cmd.CombinedOutput()
		if err != nil {
			lastError = fmt.Errorf("yt-dlp error (strategy %s): %v, details: %s", strategy.name, err, string(output))
			fmt.Printf("[DOWNLOADER] [%s] Пользователь %d: стратегия %s не удалась: %v\n", requestID, userID, strategy.name, err)
			continue
		}

		// Проверяем, создался ли файл
		if _, err := os.Stat(absFilename); err == nil {
			fmt.Printf("[DOWNLOADER] [%s] Пользователь %d: успешно скачано с помощью стратегии: %s\n", requestID, userID, strategy.name)
			return absFilename, nil
		}

		// Ищем файл с другим расширением
		baseName := strings.TrimSuffix(absFilename, ".mp4")
		possibleExtensions := []string{".mp4", ".mkv", ".webm", ".avi", ".mov"}

		for _, ext := range possibleExtensions {
			altFilename := baseName + ext
			if _, err := os.Stat(altFilename); err == nil {
				fmt.Printf("[DOWNLOADER] [%s] Пользователь %d: найден файл с расширением %s: %s\n", requestID, userID, ext, altFilename)
				return altFilename, nil
			}
		}

		lastError = fmt.Errorf("файл не был создан после стратегии %s, yt-dlp output: %s", strategy.name, string(output))
	}

	return "", fmt.Errorf("все стратегии скачивания не удались для пользователя %d (request %s, URL %s). Последняя ошибка: %v", userID, requestID, urlHash, lastError)
}
