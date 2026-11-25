package addons

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func TestUnzipCommands(t *testing.T) {
	if got := (Unzip{}).Commands(); len(got) != 1 || got[0] != "unzip" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestUnzipEsZip(t *testing.T) {
	if !unzipEsZip(&waE2E.DocumentMessage{FileName: proto.String("datos.ZIP")}) {
		t.Fatal("extensión .zip no detectada")
	}
	if !unzipEsZip(&waE2E.DocumentMessage{Mimetype: proto.String("application/zip")}) {
		t.Fatal("mime zip no detectado")
	}
	if unzipEsZip(&waE2E.DocumentMessage{FileName: proto.String("datos.rar")}) {
		t.Fatal("rar no debe aceptarse")
	}
	if unzipEsZip(nil) {
		t.Fatal("nil no debe aceptarse")
	}
}

func TestUnzipExtraerRaiz(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entradas := map[string]string{
		"b.txt":        "contenido b",
		"a.txt":        "contenido a",
		"carpeta/x.md": "anidado",
	}
	for nombre, contenido := range entradas {
		w, err := zw.Create(nombre)
		if err != nil {
			t.Fatalf("zip create: %v", err)
		}
		if _, err := w.Write([]byte(contenido)); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}

	archivos, err := unzipExtraerRaiz(buf.Bytes())
	if err != nil {
		t.Fatalf("unzipExtraerRaiz: %v", err)
	}
	if len(archivos) != 2 {
		t.Fatalf("archivos=%#v", archivos)
	}
	if archivos[0].Nombre != "a.txt" || string(archivos[0].Datos) != "contenido a" {
		t.Fatalf("orden/contenido inesperado: %#v", archivos[0])
	}
	if archivos[1].Nombre != "b.txt" {
		t.Fatalf("segundo archivo=%#v", archivos[1])
	}
}

func TestUnzipExtraerRaizInvalido(t *testing.T) {
	if _, err := unzipExtraerRaiz([]byte("no es un zip")); err == nil {
		t.Fatal("se esperaba error")
	}
}

func TestUnzipSinDocumento(t *testing.T) {
	c := &capture{}
	if err := (Unzip{}).Handle(c.newCtx("unzip")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "❓" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "caption de un .zip") {
		t.Fatalf("textos=%#v", c.texts)
	}
}

func TestUnzipNoEsZip(t *testing.T) {
	ctx := wagoCtx("1205@g.us")
	ctx.Message.Message = &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
		FileName: proto.String("datos.rar"),
		Mimetype: proto.String("application/x-rar"),
	}}
	c := &capture{}
	ctx.SendHook = func(m *waE2E.Message) error { c.texts = append(c.texts, messageText(m)); return nil }
	ctx.ReactHook = func(e string) error { c.reactions = append(c.reactions, e); return nil }

	if err := (Unzip{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "❌" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "Solo acepto archivadores .zip") {
		t.Fatalf("textos=%#v", c.texts)
	}
}

func TestUnzipRemitente(t *testing.T) {
	chat := types.NewJID("1205", types.GroupServer)
	sender := types.NewJID("584141234567", types.DefaultUserServer)
	got := unzipRemitente(chat, sender)
	if got.User != "584141234567" || got.Server != types.DefaultUserServer {
		t.Fatalf("remitente=%v", got)
	}
	got = unzipRemitente(chat, types.EmptyJID)
	if got.User != "1205" {
		t.Fatalf("fallback=%v", got)
	}
}
