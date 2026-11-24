// ss.go porta addons/ss.js: captura de pantalla de una página web con
// Chrome/Chromium headless mediante chromedp.
package addons

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/chromedp"

	"whatsbot-go/internal/core"
)

const (
	ssWidth      = 1280
	ssHeight     = 2400
	ssTimeout    = 60 * time.Second
	ssMinBytes   = 1000
	ssChromeFlag = "SS_CHROME_BIN"
)

// ssChromePath permite forzar el binario; vacío = autodetección.
var ssChromePath string

// ssCapture y ssLookPath son inyectables en pruebas.
var ssLookPath = func(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

var ssCapture = func(url, outPath string) error {
	bin := ssResolveChrome()
	if bin == "" {
		return fmt.Errorf("no hay navegador disponible")
	}
	return ssShotWithChrome(bin, url, outPath)
}

// Ss porta los comandos .ss y .screenshot.
type Ss struct{}

// Commands cumple core.Addon.
func (Ss) Commands() []string { return []string{"ss", "screenshot"} }

// Handle captura la URL indicada.
func (s Ss) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	raw := strings.TrimSpace(strings.Join(ctx.Args, " "))
	target := ssNormalizeURL(raw)
	if target == "" {
		if reactErr := ctx.React("⚠️"); reactErr != nil {
			return reactErr
		}
		return ctx.Reply("📸 Captura páginas web.\n\n• `.ss google.com`\n• `.ss https://x.com/usuario`")
	}

	if reactErr := ctx.React("📸"); reactErr != nil {
		return reactErr
	}

	tmpDir, err := os.MkdirTemp("", "ss_")
	if err != nil {
		return ssFail(ctx, err)
	}
	defer os.RemoveAll(tmpDir)

	outPath := filepath.Join(tmpDir, "shot.png")
	if err := ssCapture(target, outPath); err != nil {
		return ssFail(ctx, err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		return ssFail(ctx, err)
	}
	if len(data) < ssMinBytes {
		return ssFail(ctx, fmt.Errorf("captura vacía"))
	}

	if err := ctx.SendImage(data, "📸 "+target); err != nil {
		return ssFail(ctx, err)
	}
	return ctx.React("✅")
}

func ssNormalizeURL(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return ""
	}
	if !regexp.MustCompile(`(?i)^https?://`).MatchString(u) {
		if regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*:`).MatchString(u) {
			return ""
		}
		u = "https://" + u
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return parsed.String()
}

func ssResolveChrome() string {
	if ssChromePath != "" {
		return ssChromePath
	}
	candidates := []string{
		os.Getenv(ssChromeFlag),
		"/usr/bin/google-chrome-stable",
		"/usr/bin/google-chrome",
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
	}
	for _, candidate := range candidates {
		if candidate != "" && ssLookPath(candidate) {
			return candidate
		}
	}
	return ""
}

func ssShotWithChrome(bin, target, outPath string) error {
	parent, cancelParent := context.WithTimeout(context.Background(), ssTimeout)
	defer cancelParent()

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(bin),
		chromedp.NoSandbox,
		chromedp.DisableGPU,
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.WindowSize(ssWidth, ssHeight),
		chromedp.Flag("virtual-time-budget", 9000),
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(parent, opts...)
	defer cancelAlloc()

	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	defer cancelCtx()

	if _, err := chromedp.Run(ctx, chromedp.EmulateViewport(ssWidth, ssHeight)); err != nil {
		return err
	}
	if _, err := chromedp.Run(ctx, chromedp.Navigate(target)); err != nil {
		return err
	}
	if _, err := chromedp.Run(ctx, chromedp.Sleep(9*time.Second)); err != nil {
		return err
	}
	png, err := chromedp.Run(ctx, chromedp.CaptureScreenshot())
	if err != nil {
		return err
	}
	if len(png) < ssMinBytes {
		return fmt.Errorf("captura vacía")
	}
	return os.WriteFile(outPath, png, 0o644)
}

func ssFail(ctx *core.Context, err error) error {
	fmt.Printf("❌ [ss]: %v\n", err)
	if reactErr := ctx.React("❌"); reactErr != nil {
		return reactErr
	}
	msg := err.Error()
	if len(msg) > 150 {
		msg = msg[:150]
	}
	return ctx.Reply("❌ No pude capturar esa página: " + msg)
}
