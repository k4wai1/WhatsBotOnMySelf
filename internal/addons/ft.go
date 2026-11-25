// ft.go porta addons/ft.js: convierte un sticker citado a PNG o GIF usando
// ImageMagick y lo envía como documento.
package addons

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"whatsbot-go/internal/core"
)

// ftCacheDir, ftConvert, ftDownload y ftNow son inyectables en pruebas.
var ftCacheDir = func() string { return core.DataPath("cache") }

var ftDownload = func(ctx *core.Context) ([]byte, error) {
	return ctx.DownloadQuoted()
}

var ftNow = time.Now

var ftConvert = func(parent context.Context, args ...string) error {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "convert", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("convert: %w: %s", err, stderr.String())
	}
	return nil
}

// Ft porta el comando .ft.
type Ft struct{}

// Commands cumple core.Addon.
func (Ft) Commands() []string { return []string{"ft"} }

// Handle convierte el sticker citado.
func (f Ft) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	quoted := ctx.QuotedMessage()
	if quoted == nil || quoted.GetStickerMessage() == nil {
		return ctx.React("❓")
	}

	cacheDir := ftCacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return ftFail(ctx, err)
	}

	if reactErr := ctx.React("⏳"); reactErr != nil {
		return reactErr
	}

	timestamp := ftNow().UnixMilli()
	inputPath := filepath.Join(cacheDir, fmt.Sprintf("input_%d.webp", timestamp))
	filesToClean := []string{inputPath}
	defer func() {
		for _, path := range filesToClean {
			if path != "" {
				_ = os.RemoveAll(path)
			}
		}
	}()

	buffer, err := ftDownload(ctx)
	if err != nil || len(buffer) == 0 {
		if err == nil {
			err = fmt.Errorf("sticker vacío")
		}
		return ftFail(ctx, err)
	}
	if err := os.WriteFile(inputPath, buffer, 0o644); err != nil {
		return ftFail(ctx, err)
	}

	isAnimated := bytes.Contains(buffer, []byte("ANIM"))
	outputExt := "png"
	if isAnimated {
		outputExt = "gif"
	}
	outputPath := filepath.Join(cacheDir, fmt.Sprintf("output_%d.%s", timestamp, outputExt))
	filesToClean = append(filesToClean, outputPath)

	if isAnimated {
		tempDir := filepath.Join(cacheDir, fmt.Sprintf("temp_%d", timestamp))
		if err := os.MkdirAll(tempDir, 0o755); err != nil {
			return ftFail(ctx, err)
		}
		filesToClean = append(filesToClean, tempDir)

		framePattern := filepath.Join(tempDir, "frame_%04d.png")
		if err := ftConvert(context.Background(), inputPath, "-coalesce", framePattern); err != nil {
			return ftFail(ctx, err)
		}
		frames, err := filepath.Glob(filepath.Join(tempDir, "frame_*.png"))
		if err != nil || len(frames) == 0 {
			if err == nil {
				err = fmt.Errorf("no se generaron frames")
			}
			return ftFail(ctx, err)
		}
		args := []string{"-dispose", "background", "-delay", "8", "-loop", "0"}
		args = append(args, frames...)
		args = append(args, "-layers", "optimize", outputPath)
		if err := ftConvert(context.Background(), args...); err != nil {
			return ftFail(ctx, err)
		}
	} else {
		if err := ftConvert(context.Background(), inputPath+"[0]", "-background", "none", outputPath); err != nil {
			return ftFail(ctx, err)
		}
	}

	finalBuffer, err := os.ReadFile(outputPath)
	if err != nil {
		return ftFail(ctx, err)
	}

	if err := ctx.SendDocument(finalBuffer, fmt.Sprintf("file_%d.%s", timestamp, outputExt), ""); err != nil {
		return ftFail(ctx, err)
	}
	return ctx.React("✅")
}

func ftFail(ctx *core.Context, err error) error {
	fmt.Printf("Error en addon .ft: %v\n", err)
	return ctx.React("❌")
}

func init() { core.Register(Ft{}) }
