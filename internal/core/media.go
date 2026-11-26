package core

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"path/filepath"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// SendImage sube y envía una imagen, con caption opcional.
func (c *Context) SendImage(data []byte, caption string) error {
	msg, err := c.buildImageMessage(data, caption)
	if err != nil {
		return err
	}
	_, err = c.sendWithID(msg)
	return err
}

// SendImageID es SendImage pero devuelve el ID del mensaje enviado.
func (c *Context) SendImageID(data []byte, caption string) (types.MessageID, error) {
	msg, err := c.buildImageMessage(data, caption)
	if err != nil {
		return "", err
	}
	return c.sendWithID(msg)
}

func (c *Context) buildImageMessage(data []byte, caption string) (*waE2E.Message, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("imagen vacía")
	}
	width, height := imageSize(data)
	resp, err := c.upload(data, whatsmeow.MediaImage)
	if err != nil {
		return nil, err
	}
	return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		URL:           proto.String(resp.URL),
		Mimetype:      proto.String(detectMime(data, "image/jpeg")),
		Caption:       optString(caption),
		FileSHA256:    resp.FileSHA256,
		FileEncSHA256: resp.FileEncSHA256,
		FileLength:    proto.Uint64(resp.FileLength),
		MediaKey:      resp.MediaKey,
		DirectPath:    proto.String(resp.DirectPath),
		Width:         proto.Uint32(width),
		Height:        proto.Uint32(height),
	}}, nil
}

// SendVideo sube y envía un video.
func (c *Context) SendVideo(data []byte, caption string, gifPlayback bool) error {
	if len(data) == 0 {
		return fmt.Errorf("video vacío")
	}
	width, height := imageSize(data)
	resp, err := c.upload(data, whatsmeow.MediaVideo)
	if err != nil {
		return err
	}
	msg := &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
		URL:           proto.String(resp.URL),
		Mimetype:      proto.String(detectMime(data, "video/mp4")),
		Caption:       optString(caption),
		FileSHA256:    resp.FileSHA256,
		FileEncSHA256: resp.FileEncSHA256,
		FileLength:    proto.Uint64(resp.FileLength),
		MediaKey:      resp.MediaKey,
		DirectPath:    proto.String(resp.DirectPath),
		Width:         proto.Uint32(width),
		Height:        proto.Uint32(height),
		GifPlayback:   proto.Bool(gifPlayback),
	}}
	return c.SendMessage(msg)
}

// SendAudio sube y envía audio. ptt=true lo manda como nota de voz.
func (c *Context) SendAudio(data []byte, mimetype string, ptt bool, seconds uint32) error {
	if len(data) == 0 {
		return fmt.Errorf("audio vacío")
	}
	if mimetype == "" {
		mimetype = detectMime(data, "audio/ogg")
	}
	resp, err := c.upload(data, whatsmeow.MediaAudio)
	if err != nil {
		return err
	}
	msg := &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
		URL:           proto.String(resp.URL),
		Mimetype:      proto.String(mimetype),
		FileSHA256:    resp.FileSHA256,
		FileEncSHA256: resp.FileEncSHA256,
		FileLength:    proto.Uint64(resp.FileLength),
		MediaKey:      resp.MediaKey,
		DirectPath:    proto.String(resp.DirectPath),
		PTT:           proto.Bool(ptt),
		Seconds:       proto.Uint32(seconds),
	}}
	return c.SendMessage(msg)
}

// SendDocument sube y envía un documento con nombre de archivo.
func (c *Context) SendDocument(data []byte, filename, caption string) error {
	if len(data) == 0 {
		return fmt.Errorf("documento vacío")
	}
	if filename == "" {
		filename = "archivo"
	}
	mimetype := detectMime(data, "application/octet-stream")
	if m := mimeByExt(filename); m != "" {
		mimetype = m
	}
	resp, err := c.upload(data, whatsmeow.MediaDocument)
	if err != nil {
		return err
	}
	msg := &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
		URL:           proto.String(resp.URL),
		Mimetype:      proto.String(mimetype),
		Title:         proto.String(filename),
		FileName:      proto.String(filename),
		Caption:       optString(caption),
		FileSHA256:    resp.FileSHA256,
		FileEncSHA256: resp.FileEncSHA256,
		FileLength:    proto.Uint64(resp.FileLength),
		MediaKey:      resp.MediaKey,
		DirectPath:    proto.String(resp.DirectPath),
	}}
	return c.SendMessage(msg)
}

// SendSticker sube y envía un sticker WebP de 512x512.
func (c *Context) SendSticker(data []byte, animated bool) error {
	if len(data) == 0 {
		return fmt.Errorf("sticker vacío")
	}
	resp, err := c.upload(data, whatsmeow.MediaImage)
	if err != nil {
		return err
	}
	msg := &waE2E.Message{StickerMessage: &waE2E.StickerMessage{
		URL:           proto.String(resp.URL),
		Mimetype:      proto.String("image/webp"),
		FileSHA256:    resp.FileSHA256,
		FileEncSHA256: resp.FileEncSHA256,
		FileLength:    proto.Uint64(resp.FileLength),
		MediaKey:      resp.MediaKey,
		DirectPath:    proto.String(resp.DirectPath),
		Width:         proto.Uint32(512),
		Height:        proto.Uint32(512),
		IsAnimated:    proto.Bool(animated),
	}}
	return c.SendMessage(msg)
}

// SendTo envía un mensaje arbitrario a otro chat.
func (c *Context) SendTo(jid types.JID, msg *waE2E.Message) error {
	if c.Client == nil || msg == nil {
		return nil
	}
	_, err := c.Client.SendMessage(context.Background(), jid, msg)
	return err
}

// ContextInfo devuelve el ContextInfo del mensaje actual (citas, menciones).
func (c *Context) ContextInfo() *waE2E.ContextInfo {
	if c.Message == nil {
		return nil
	}
	return contextInfoOf(c.Message.Message)
}

// MentionedJIDs devuelve las menciones del mensaje actual.
func (c *Context) MentionedJIDs() []types.JID {
	info := c.ContextInfo()
	if info == nil {
		return nil
	}
	return ParseJIDs(info.GetMentionedJID())
}

// ParseJIDs convierte cadenas JID en types.JID válidos, descartando las malas.
func ParseJIDs(raw []string) []types.JID {
	out := make([]types.JID, 0, len(raw))
	for _, s := range raw {
		jid, err := types.ParseJID(s)
		if err == nil {
			out = append(out, jid)
		}
	}
	return out
}

// DownloadQuoted descarga el multimedia del mensaje citado.
func (c *Context) DownloadQuoted() ([]byte, error) {
	quoted := c.QuotedMessage()
	target := downloadableMedia(quoted)
	if target == nil {
		return nil, fmt.Errorf("el mensaje citado no tiene multimedia")
	}
	if c.Client == nil {
		return nil, fmt.Errorf("cliente no disponible")
	}
	return c.Client.Download(context.Background(), target)
}

// Download descarga un sub-mensaje multimedia concreto.
func (c *Context) Download(msg *waE2E.Message) ([]byte, error) {
	target := downloadableMedia(msg)
	if target == nil {
		return nil, fmt.Errorf("el mensaje no tiene multimedia")
	}
	if c.Client == nil {
		return nil, fmt.Errorf("cliente no disponible")
	}
	return c.Client.Download(context.Background(), target)
}

func (c *Context) upload(data []byte, mt whatsmeow.MediaType) (whatsmeow.UploadResponse, error) {
	// En pruebas (SendHook) no hay cliente: se envía el mensaje sin subir.
	if c.Client == nil {
		if c.SendHook != nil {
			return whatsmeow.UploadResponse{FileLength: uint64(len(data))}, nil
		}
		return whatsmeow.UploadResponse{}, fmt.Errorf("cliente no disponible")
	}
	return c.Client.Upload(context.Background(), data, mt)
}

func imageSize(data []byte) (uint32, uint32) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0
	}
	return uint32(cfg.Width), uint32(cfg.Height)
}

func detectMime(data []byte, fallback string) string {
	if len(data) == 0 {
		return fallback
	}
	sniff := data
	if len(sniff) > 512 {
		sniff = sniff[:512]
	}
	detected := http.DetectContentType(sniff)
	if detected == "" || detected == "application/octet-stream" {
		return fallback
	}
	return detected
}

func mimeByExt(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".pdf":
		return "application/pdf"
	case ".zip":
		return "application/zip"
	case ".json":
		return "application/json"
	case ".txt":
		return "text/plain"
	case ".mp3":
		return "audio/mpeg"
	case ".ogg", ".opus":
		return "audio/ogg"
	case ".mp4":
		return "video/mp4"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".csv":
		return "text/csv"
	}
	return ""
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return proto.String(s)
}
