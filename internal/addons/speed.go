package addons

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"go.mau.fi/whatsmeow/types"

	"whatsbot-go/internal/core"
)

const speedMaxBytes = 25 * 1024 * 1024

// Speed porta addons/speed.js. Solo dueños.
type Speed struct{}

func init() { core.Register(Speed{}) }

func (Speed) Commands() []string { return []string{"speed"} }

var (
	speedHTTP = &http.Client{Timeout: 60 * time.Second}
	speedURL  = "https://speed.cloudflare.com/__down?bytes=10000000"
)

type speedResult struct {
	Mbps    float64
	Latency float64
}

// speedMeasure descarga speedURL y calcula Mbps + latencia.
func speedMeasure() (speedResult, error) {
	start := time.Now()
	req, err := http.NewRequest(http.MethodGet, speedURL, nil)
	if err != nil {
		return speedResult{}, err
	}
	req.Header.Set("User-Agent", "WhatsBotOnMySelf/1.0")
	resp, err := speedHTTP.Do(req)
	if err != nil {
		return speedResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return speedResult{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	latency := time.Since(start)
	readStart := time.Now()
	n, err := io.Copy(io.Discard, io.LimitReader(resp.Body, speedMaxBytes))
	if err != nil {
		return speedResult{}, err
	}
	elapsed := time.Since(readStart).Seconds()
	if elapsed <= 0 {
		elapsed = 0.001
	}
	return speedResult{
		Mbps:    float64(n) * 8 / elapsed / 1_000_000,
		Latency: float64(latency.Milliseconds()),
	}, nil
}

var speedSpin = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

func (Speed) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	isOwner := false
	for _, owner := range wagoOwnerNumbers() {
		if owner == ctx.SenderJID().User {
			isOwner = true
			break
		}
	}
	if !isOwner {
		return ctx.React("⛔")
	}

	var textoID string
	if ctx.Client != nil {
		resp, err := ctx.Client.SendMessage(context.Background(), ctx.ChatJID(), core.TextMessage("🚀 Iniciando Speedtest..."))
		if err == nil {
			textoID = string(resp.ID)
		}
	} else {
		_ = ctx.Reply("🚀 Iniciando Speedtest...")
	}

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(700 * time.Millisecond)
		defer ticker.Stop()
		frame := 0
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if ctx.Client != nil && textoID != "" {
					edit := ctx.Client.BuildEdit(ctx.ChatJID(), types.MessageID(textoID),
						core.TextMessage(speedSpin[frame%len(speedSpin)]+" *Calculando velocidad...*"))
					_, _ = ctx.Client.SendMessage(context.Background(), ctx.ChatJID(), edit)
				}
				frame++
			}
		}
	}()

	result, err := speedMeasure()
	close(done)

	if err != nil {
		slog.Error("❌ [speed] Error del test:", "error", err)
		return ctx.Reply("❌ Error al ejecutar el test.")
	}

	text := "📊 *Resultado Speedtest*\n\n" +
		fmt.Sprintf("⬇️ Descarga: *%s Mbps*\n", strconv.FormatFloat(result.Mbps, 'f', 2, 64)) +
		fmt.Sprintf("⬆️ Subida: *%s Mbps*\n", strconv.FormatFloat(result.Mbps, 'f', 2, 64)) +
		fmt.Sprintf("⏱️ Latencia: *%s ms*", strconv.FormatFloat(result.Latency, 'f', 0, 64))
	return ctx.Reply(text)
}
