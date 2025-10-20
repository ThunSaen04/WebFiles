package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/golang-jwt/jwt/v5"
	"github.com/joho/godotenv"
)

type FileMeta struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	Path     string `json:"-"`
}

type FileStore struct {
	Files []FileMeta `json:"files"`
	mu    sync.Mutex `json:"-"`
}

type LoginRequest struct {
	PIN string `json:"pin"`
}

const (
	uploadDir    = "./uploads"
	metadataFile = "./filedata.json"
)

var webfiles FileStore

var correctPIN string
var jwtSecret []byte

func loadEnv() {
	err := godotenv.Load()
	if err != nil {
		log.Println("Warning: .env file not found, using default or system environment variables.")
	}

	correctPIN = os.Getenv("LOGIN_PIN")
	if correctPIN == "" {
		log.Fatal("Error: LOGIN_PIN is not set in the environment.")
	}

	jwtSecretStr := os.Getenv("JWT_SECRET_KEY")
	if jwtSecretStr == "" {
		log.Fatal("Error: JWT_SECRET_KEY is not set in the environment.")
	}
	jwtSecret = []byte(jwtSecretStr)

	log.Println("Environment variables loaded successfully.")
}

func main() {
	log.Println("Starting File Share Server on :3002 ...")

	loadEnv()

	app := fiber.New(fiber.Config{
		BodyLimit: 2 * 1024 * 1024 * 1024,
	})

	loadMetadata()

	app.Use(func(c *fiber.Ctx) error {
		if c.Path() == "/login" || c.Path() == "/logout" || strings.HasPrefix(c.Path(), "/public") {
			return c.Next()
		}

		tokenString := c.Cookies("session")
		if tokenString == "" {
			log.Println("[AUTH] No session cookie found, redirecting to login.")
			return c.Redirect("/login")
		}

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return jwtSecret, nil
		})

		if err != nil || !token.Valid {
			log.Println("[AUTH] Invalid or expired token, redirecting to login.")
			c.ClearCookie("session")
			return c.Redirect("/login")
		}

		return c.Next()
	})

	app.Static("/", "./public", fiber.Static{Index: "index.html"})
	app.Static("/login", "./public", fiber.Static{Index: "login.html"})

	loginLimiter := limiter.New(limiter.Config{
		Max:        5,
		Expiration: 1 * time.Minute,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
	})

	app.Post("/login", loginLimiter, func(c *fiber.Ctx) error {
		var req LoginRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request"})
		}
		if req.PIN != correctPIN {
			log.Printf("[AUTH] Failed login attempt with PIN: %s", req.PIN)
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Incorrect PIN"})
		}

		log.Println("[AUTH] Login successful.")
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"exp": time.Now().Add(time.Hour * 24).Unix(),
			"pin": req.PIN,
		})

		tokenString, err := token.SignedString(jwtSecret)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to generate token"})
		}

		c.Cookie(&fiber.Cookie{
			Name:     "session",
			Value:    tokenString,
			Expires:  time.Now().Add(time.Hour * 24),
			HTTPOnly: true,
			Secure:   true,
			SameSite: fiber.CookieSameSiteStrictMode,
		})
		return c.JSON(fiber.Map{"status": "ok"})
	})

	app.Get("/logout", func(c *fiber.Ctx) error {
		log.Println("[AUTH] User logged out.")
		c.ClearCookie("session")
		return c.Redirect("/login")
	})

	app.Post("/upload", uploadHandler)
	app.Get("/files", filesHandler)
	app.Get("/download/:filename", downloadHandler)
	app.Delete("/delete/:filename", deleteHandler)

	log.Fatal(app.Listen(":3002"))
}

// --- Handlers ---

func uploadHandler(c *fiber.Ctx) error {
	log.Println("\n--- [DEBUG] STARTING UPLOAD HANDLER ---")
	file, err := c.FormFile("file")
	if err != nil {
		log.Printf("[DEBUG] 1. ERROR: Could not get file from form: %v\n", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	log.Printf("[DEBUG] 1. Received file from form: '%s' (Size: %d bytes)\n", file.Filename, file.Size)

	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		log.Printf("[DEBUG] ERROR: Could not create upload directory '%s': %v\n", uploadDir, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not create upload directory"})
	}

	originalName := file.Filename

	cleanedFilename := filepath.Base(originalName)
	if cleanedFilename == "." || cleanedFilename == "/" {
		log.Println("[SECURITY] Invalid filename received:", originalName)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid filename"})
	}

	finalFilename := cleanedFilename
	filePath := filepath.Join(uploadDir, finalFilename)
	log.Printf("[DEBUG] 2. Sanitized file path set to: '%s'\n", filePath)

	if _, err := os.Stat(filePath); err == nil {
		log.Printf("[DEBUG] 3. File '%s' already exists. Generating a new name.\n", finalFilename)
		ext := ""
		name := originalName
		if dotIndex := strings.LastIndex(originalName, "."); dotIndex != -1 {
			name = originalName[:dotIndex]
			ext = originalName[dotIndex:]
		}
		finalFilename = fmt.Sprintf("%s_%d%s", name, time.Now().UnixNano(), ext)
		filePath = fmt.Sprintf("%s/%s", uploadDir, finalFilename)
		log.Printf("[DEBUG]    - New filename: '%s'\n", finalFilename)
		log.Printf("[DEBUG]    - New file path: '%s'\n", filePath)
	} else {
		log.Println("[DEBUG] 3. File does not exist. Using original name.")
	}

	if err := c.SaveFile(file, filePath); err != nil {
		log.Printf("[DEBUG] 4. ERROR: Failed to save file to '%s': %v\n", filePath, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	log.Printf("[DEBUG] 4. File successfully saved to: '%s'\n", filePath)

	meta := FileMeta{
		Filename: finalFilename,
		Size:     file.Size,
		Path:     filePath,
	}
	log.Printf("[DEBUG] 5. Created new metadata: {Filename: '%s', Size: %d, Path: '%s'}\n", meta.Filename, meta.Size, meta.Path)

	webfiles.Files = append(webfiles.Files, meta)
	if err := saveMetadata(); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save metadata"})
	}

	log.Println("--- [DEBUG] ENDING UPLOAD HANDLER ---")
	return c.JSON(fiber.Map{"status": "uploaded", "filename": meta.Filename, "size": meta.Size})
}

func filesHandler(c *fiber.Ctx) error {
	webfiles.mu.Lock()
	defer webfiles.mu.Unlock()
	log.Printf("[API] Listing files. Total count: %d\n", len(webfiles.Files))
	return c.JSON(webfiles.Files)
}

func downloadHandler(c *fiber.Ctx) error {
	log.Println("\n--- [DEBUG] STARTING DOWNLOAD HANDLER ---")

	rawFilename := c.Params("filename")
	log.Printf("[DEBUG] 1. Received raw filename from URL: '%s'\n", rawFilename)

	requestedFilename, err := url.QueryUnescape(rawFilename)
	if err != nil {
		log.Println("[DEBUG] ERROR: Failed to decode filename.")
		return c.Status(fiber.StatusBadRequest).SendString("Invalid filename")
	}
	log.Printf("[DEBUG] 2. Decoded filename: '%s'\n", requestedFilename)

	webfiles.mu.Lock()
	defer webfiles.mu.Unlock()

	log.Println("[DEBUG] 3. Starting search in web files...")
	var foundFile *FileMeta
	for i := range webfiles.Files {
		webfilesFilename := webfiles.Files[i].Filename
		log.Printf("[DEBUG]    - Comparing with web file: '%s'\n", webfilesFilename)
		if webfilesFilename == requestedFilename {
			log.Println("[DEBUG]    *** MATCH FOUND! ***")
			foundFile = &webfiles.Files[i]
			break
		}
	}

	if foundFile == nil {
		log.Println("[DEBUG] 4. ERROR: No match found in metadata webfiles.")
		log.Println("--- [DEBUG] ENDING DOWNLOAD HANDLER ---")
		return c.Status(fiber.StatusNotFound).SendString("File not found in metadata")
	}

	log.Printf("[DEBUG] 4. Match found. File path from metadata is: '%s'\n", foundFile.Path)

	if _, err := os.Stat(foundFile.Path); os.IsNotExist(err) {
		log.Printf("[DEBUG] 5. ERROR: File path '%s' does NOT exist on disk!\n", foundFile.Path)
		log.Println("--- [DEBUG] ENDING DOWNLOAD HANDLER ---")
		return c.Status(fiber.StatusNotFound).SendString("File not found on disk")
	}

	log.Printf("[DEBUG] 5. File exists on disk. Proceeding to download.\n")
	log.Println("--- [DEBUG] ENDING DOWNLOAD HANDLER ---")

	return c.Download(foundFile.Path, foundFile.Filename)
}

func deleteHandler(c *fiber.Ctx) error {
	log.Println("\n--- [DEBUG] STARTING DELETE HANDLER ---")

	rawFilename := c.Params("filename")
	requestedFilename, err := url.QueryUnescape(rawFilename)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid filename"})
	}
	log.Printf("[DEBUG] Decoded filename: '%s'\n", requestedFilename)

	webfiles.mu.Lock()
	defer webfiles.mu.Unlock() // Lock is acquired here

	var fileIndex = -1
	for i, f := range webfiles.Files {
		if f.Filename == requestedFilename {
			fileIndex = i
			break
		}
	}

	if fileIndex == -1 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "File not found in metadata"})
	}

	filePathToDelete := filepath.Join(uploadDir, requestedFilename)
	if err := os.Remove(filePathToDelete); err != nil && !os.IsNotExist(err) {
		log.Printf("[DEBUG] WARNING: Could not delete file from disk: %v\n", err)
	} else {
		log.Printf("[DEBUG] Successfully deleted file from disk: '%s'\n", filePathToDelete)
	}

	webfiles.Files = append(webfiles.Files[:fileIndex], webfiles.Files[fileIndex+1:]...)
	log.Println("[DEBUG] Removed file metadata from webfiles slice.")

	// --- [FIX] Call the UNLOCKED version here to avoid deadlock ---
	if err := saveMetadataUnlocked(); err != nil {
		log.Println("[DEBUG] ERROR: Failed to save metadata after deletion.")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update metadata"})
	}

	log.Println("--- [DEBUG] ENDING DELETE HANDLER ---")
	return c.JSON(webfiles.Files)
}

// --- Metadata Functions ---

// saveMetadataUnlocked performs the save operation without handling mutex locks.
// This should be called by functions that have already acquired the lock.
func saveMetadataUnlocked() error {
	log.Println("[DEBUG] Saving metadata to file (unlocked)...")

	dataToSave := struct {
		Files []FileMeta `json:"files"`
	}{
		Files: webfiles.Files,
	}

	data, err := json.MarshalIndent(dataToSave, "", "  ")
	if err != nil {
		log.Printf("[DEBUG] ERROR: Failed to marshal metadata to JSON: %v\n", err)
		return err
	}
	if err := os.WriteFile(metadataFile, data, 0644); err != nil {
		log.Printf("[DEBUG] ERROR: Failed to write metadata to file '%s': %v\n", metadataFile, err)
		return err
	}
	log.Println("[DEBUG] Metadata saved successfully.")
	return nil
}

// --- Metadata Functions ---

func saveMetadata() error {
	webfiles.mu.Lock()
	defer webfiles.mu.Unlock()

	// log.Println("[DEBUG] Saving metadata to file...")

	// dataToSave := struct {
	// 	Files []FileMeta `json:"files"`
	// }{
	// 	Files: webfiles.Files,
	// }

	// data, err := json.MarshalIndent(dataToSave, "", "  ")
	// if err != nil {
	// 	log.Printf("[DEBUG] ERROR: Failed to marshal metadata to JSON: %v\n", err)
	// 	return err
	// }
	// if err := os.WriteFile(metadataFile, data, 0644); err != nil {
	// 	log.Printf("[DEBUG] ERROR: Failed to write metadata to file '%s': %v\n", metadataFile, err)
	// 	return err
	// }
	// log.Println("[DEBUG] Metadata saved successfully.")

	return saveMetadataUnlocked()
}

func loadMetadata() {
	webfiles.mu.Lock()
	defer webfiles.mu.Unlock()

	log.Println("[DEBUG] Attempting to load metadata from file...")
	if _, err := os.Stat(metadataFile); os.IsNotExist(err) {
		log.Println("[DEBUG] Metadata file not found, starting fresh.")
		return
	}
	data, err := os.ReadFile(metadataFile)
	if err != nil {
		log.Printf("[DEBUG] ERROR: Failed to read metadata file '%s': %v\n", metadataFile, err)
		return
	}
	if err := json.Unmarshal(data, &webfiles); err != nil {
		log.Printf("[DEBUG] ERROR: Failed to unmarshal JSON data from metadata file: %v\n", err)
		return
	}
	log.Printf("[DEBUG] Metadata loaded successfully. Total files: %d\n", len(webfiles.Files))
}
