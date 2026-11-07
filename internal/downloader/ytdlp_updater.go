package downloader

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

var ytdlpUpdateMutex sync.Mutex

func installedYTDLPVersion() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, ytdlpPath(), "--ignore-config", "--version").Output()
	return strings.TrimPrefix(strings.TrimSpace(string(output)), "v"), err
}

func sameYTDLPVersion(recorded, installed string) bool {
	return installed != "" && strings.TrimPrefix(recorded, "v") == strings.TrimPrefix(installed, "v")
}

// GitHubRelease represents a GitHub release
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// YTDLpVersion represents a version record in the database
type YTDLpVersion struct {
	ID          int64      `json:"id"`
	Version     string     `json:"version"`
	LastChecked time.Time  `json:"last_checked"`
	LastUpdated *time.Time `json:"last_updated"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

const (
	// GitHub API URL for yt-dlp releases
	githubReleasesURL = "https://api.github.com/repos/yt-dlp/yt-dlp/releases/latest"

	// Update check interval (24 hours)
	updateCheckInterval = 24 * time.Hour
)

// CheckAndUpdateYTDLp проверяет наличие новой версии yt-dlp и обновляет её при необходимости
func CheckAndUpdateYTDLp(db *sql.DB) error {
	ytdlpUpdateMutex.Lock()
	defer ytdlpUpdateMutex.Unlock()
	if db == nil {
		return fmt.Errorf("yt-dlp update: database is unavailable")
	}
	// Проверяем, нужно ли обновлять (прошла ли 24 часа с последней проверки)
	shouldCheck, err := shouldCheckForUpdate(db)
	if err != nil {
		return fmt.Errorf("ошибка проверки необходимости обновления: %w", err)
	}

	// Проверяем, есть ли запись в БД
	currentVersion, err := getCurrentVersion(db)
	if err != nil {
		return fmt.Errorf("ошибка получения текущей версии из БД: %w", err)
	}
	installed, installedErr := installedYTDLPVersion()
	// Rebuilding a container can restore an older binary while the DB stays current.
	if !shouldCheck && installedErr == nil && currentVersion != nil && sameYTDLPVersion(currentVersion.Version, installed) {
		return nil
	}
	latestRelease, err := getLatestReleaseInfo()
	if err != nil {
		return fmt.Errorf("ошибка получения информации о последней версии: %w", err)
	}

	// Если версия отличается или отсутствует, скачиваем новую
	if installedErr != nil || !sameYTDLPVersion(latestRelease.TagName, installed) {
		if err := downloadAndUpdateYTDLp(latestRelease); err != nil {
			return fmt.Errorf("ошибка обновления yt-dlp: %w", err)
		}

		// Обновляем запись в БД
		if err := updateVersionRecord(db, latestRelease.TagName); err != nil {
			return fmt.Errorf("ошибка обновления записи в БД: %w", err)
		}

		fmt.Printf("[YT-DLP UPDATER] yt-dlp успешно обновлен до версии %s\n", latestRelease.TagName)
	} else {
		// Обновляем время последней проверки
		if err := updateVersionRecord(db, latestRelease.TagName); err != nil {
			return fmt.Errorf("ошибка обновления времени последней проверки: %w", err)
		}

		fmt.Printf("[YT-DLP UPDATER] yt-dlp уже последней версии: %s\n", installed)
	}

	return nil
}

// ForceUpdateYTDLp принудительно обновляет yt-dlp до последней версии, игнорируя проверки
func ForceUpdateYTDLp(db *sql.DB) error {
	ytdlpUpdateMutex.Lock()
	defer ytdlpUpdateMutex.Unlock()
	// Получаем информацию о последней версии с GitHub
	latestRelease, err := getLatestReleaseInfo()
	if err != nil {
		return fmt.Errorf("ошибка получения информации о последней версии: %w", err)
	}

	// Скачиваем и обновляем файлы yt-dlp
	if err := downloadAndUpdateYTDLp(latestRelease); err != nil {
		return fmt.Errorf("ошибка обновления yt-dlp: %w", err)
	}

	// Обновляем запись в БД
	if err := updateVersionRecord(db, latestRelease.TagName); err != nil {
		return fmt.Errorf("ошибка обновления записи в БД: %w", err)
	}

	fmt.Printf("[YT-DLP UPDATER] yt-dlp принудительно обновлен до версии %s\n", latestRelease.TagName)
	return nil
}

// shouldCheckForUpdate проверяет, прошло ли достаточно времени с последней проверки
func shouldCheckForUpdate(db *sql.DB) (bool, error) {
	row := db.QueryRow("SELECT last_checked FROM ytdlp_versions ORDER BY last_checked DESC LIMIT 1")

	var lastChecked time.Time
	err := row.Scan(&lastChecked)
	if err != nil {
		if err == sql.ErrNoRows {
			// Нет записей в БД, нужно проверить
			return true, nil
		}
		return false, err
	}

	// Проверяем, прошло ли 24 часа
	return time.Since(lastChecked) >= updateCheckInterval, nil
}

// getLatestReleaseInfo получает информацию о последней версии с GitHub
func getLatestReleaseInfo() (*GitHubRelease, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(githubReleasesURL)
	if err != nil {
		return nil, fmt.Errorf("ошибка запроса к GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("неожиданный статус ответа от GitHub API: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа: %w", err)
	}

	var release GitHubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("ошибка парсинга JSON: %w", err)
	}

	return &release, nil
}

// getCurrentVersion получает текущую версию из БД
func getCurrentVersion(db *sql.DB) (*YTDLpVersion, error) {
	row := db.QueryRow("SELECT id, version, last_checked, last_updated, created_at, updated_at FROM ytdlp_versions ORDER BY last_checked DESC LIMIT 1")

	var version YTDLpVersion
	var lastUpdated *time.Time

	err := row.Scan(&version.ID, &version.Version, &version.LastChecked, &lastUpdated, &version.CreatedAt, &version.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Нет записей в БД
		}
		return nil, err
	}

	version.LastUpdated = lastUpdated
	return &version, nil
}

// downloadAndUpdateYTDLp скачивает и обновляет файлы yt-dlp
func downloadAndUpdateYTDLp(release *GitHubRelease) error {
	// Определяем имя файла в зависимости от ОС
	var fileName string
	var tempFileName string

	if runtime.GOOS == "windows" {
		fileName = "yt-dlp.exe"
		tempFileName = "yt-dlp.exe.tmp"
	} else {
		fileName = "yt-dlp_linux"
		tempFileName = "yt-dlp_linux.tmp"
	}
	if configured := os.Getenv("YTDLP_PATH"); configured != "" {
		fileName = configured
		tempFileName = configured + ".tmp"
	}
	defer os.Remove(tempFileName)

	// Ищем нужный ассет в релизе
	var downloadURL string
	for _, asset := range release.Assets {
		assetName := "yt-dlp_linux"
		if runtime.GOOS == "windows" {
			assetName = "yt-dlp.exe"
		}
		if asset.Name == assetName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("файл %s не найден в релизе", fileName)
	}

	// Скачиваем файл во временное имя
	if err := downloadFile(downloadURL, tempFileName); err != nil {
		return fmt.Errorf("ошибка скачивания файла: %w", err)
	}

	// Делаем файл исполняемым (для Linux)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tempFileName, 0755); err != nil {
			return fmt.Errorf("ошибка установки прав доступа: %w", err)
		}
	}

	// Заменяем оригинальный файл
	if err := os.Rename(tempFileName, fileName); err != nil {
		return fmt.Errorf("ошибка замены файла: %w", err)
	}

	return nil
}

// downloadFile скачивает файл по URL
func downloadFile(url, fileName string) error {
	client := &http.Client{
		Timeout: 5 * time.Minute,
	}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("ошибка запроса файла: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("неожиданный статус ответа: %d", resp.StatusCode)
	}

	out, err := os.Create(fileName)
	if err != nil {
		return fmt.Errorf("ошибка создания файла: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("ошибка записи файла: %w", err)
	}

	return nil
}

// updateVersionRecord обновляет или создает запись о версии в БД
func updateVersionRecord(db *sql.DB, version string) error {
	now := time.Now()

	// Проверяем, существует ли запись
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM ytdlp_versions WHERE version = $1", version).Scan(&count)
	if err != nil {
		return err
	}

	if count > 0 {
		// Обновляем существующую запись
		_, err = db.Exec("UPDATE ytdlp_versions SET last_checked = $1, last_updated = $2, updated_at = $3 WHERE version = $4",
			now, now, now, version)
	} else {
		// Создаем новую запись
		_, err = db.Exec("INSERT INTO ytdlp_versions (version, last_checked, last_updated, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)",
			version, now, now, now, now)
	}

	return err
}

// updateLastChecked обновляет время последней проверки
func updateLastChecked(db *sql.DB, version string) error {
	now := time.Now()
	_, err := db.Exec("UPDATE ytdlp_versions SET last_checked = $1, updated_at = $2 WHERE version = $3",
		now, now, version)
	return err
}
