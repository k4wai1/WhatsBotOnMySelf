package addons

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"whatsbot-go/internal/core"
)

// Server porta addons/server.js: estado de un servidor de Minecraft.
type Server struct{}

func init() { core.Register(Server{}) }

func (Server) Commands() []string { return []string{"server"} }

var (
	serverPingFn      = serverPing
	serverLookupSRVFn = func(host string) (string, int, bool) {
		_, records, err := net.LookupSRV("minecraft", "tcp", host)
		if err != nil || len(records) == 0 {
			return "", 0, false
		}
		return strings.TrimSuffix(records[0].Target, "."), int(records[0].Port), true
	}
	serverColorRe = regexp.MustCompile(`(?i)§[0-9a-fk-or]`)
)

// serverParseTarget separa "ip:puerto" con el puerto por defecto 25565.
func serverParseTarget(args []string) (string, int) {
	target := "FocaTostada.aternos.me"
	if v := strings.TrimSpace(getenvServerDefault()); v != "" {
		target = v
	}
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		target = strings.TrimSpace(args[0])
	}
	port := 25565
	if idx := strings.LastIndex(target, ":"); idx > 0 {
		if p, err := strconv.Atoi(target[idx+1:]); err == nil && p > 0 && p <= 65535 {
			port = p
			target = target[:idx]
		}
	}
	return target, port
}

func getenvServerDefault() string { return wagoGetenv("MC_DEFAULT_SERVER", "") }

type serverStatus struct {
	Version struct {
		Name string `json:"name"`
	} `json:"version"`
	Players struct {
		Online int `json:"online"`
		Max    int `json:"max"`
		Sample []struct {
			Name string `json:"name"`
		} `json:"sample"`
	} `json:"players"`
}

func (Server) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	targetIP, targetPort := serverParseTarget(ctx.Args)
	if err := ctx.React("⏳"); err != nil {
		return err
	}

	finalHost, finalPort := targetIP, targetPort
	if targetPort == 25565 && strings.Contains(strings.ToLower(targetIP), "aternos.me") {
		if host, port, ok := serverLookupSRVFn(targetIP); ok {
			finalHost, finalPort = host, port
		}
	}

	result, err := serverPingFn(finalHost, finalPort)
	if err != nil {
		_ = ctx.Reply(fmt.Sprintf("🔴 El servidor *%s* está apagado o es inaccesible.\n\n_Aternos bloquea activamente las consultas desde la IP del VPS._", targetIP))
		return ctx.React("❌")
	}

	var b strings.Builder
	b.WriteString("🟢 *Servidor Activo* 🟢\n\n")
	fmt.Fprintf(&b, "*IP:* %s\n", targetIP)
	fmt.Fprintf(&b, "*Puerto Real:* %d\n", finalPort)
	fmt.Fprintf(&b, "*Versión:* %s\n", result.Version.Name)
	fmt.Fprintf(&b, "*Jugadores:* %d / %d\n", result.Players.Online, result.Players.Max)
	if len(result.Players.Sample) > 0 {
		b.WriteString("\n*Jugadores conectados:*\n")
		for _, player := range result.Players.Sample {
			fmt.Fprintf(&b, "🎮 %s\n", serverColorRe.ReplaceAllString(player.Name, ""))
		}
	}
	if err := ctx.Reply(b.String()); err != nil {
		return err
	}
	return ctx.React("✅")
}

// serverPing implementa el handshake + status del protocolo de Minecraft.
func serverPing(host string, port int) (serverStatus, error) {
	var out serverStatus
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return out, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Handshake
	var handshake []byte
	handshake = append(handshake, serverVarint(0x00)...)
	handshake = append(handshake, serverVarint(767)...)
	handshake = append(handshake, serverString(host)...)
	handshake = append(handshake, byte(port>>8), byte(port))
	handshake = append(handshake, serverVarint(1)...)
	if _, err := conn.Write(serverVarint(len(handshake))); err != nil {
		return out, err
	}
	if _, err := conn.Write(handshake); err != nil {
		return out, err
	}
	// Status request
	request := append(serverVarint(0x00), 0x00)
	if _, err := conn.Write(request); err != nil {
		return out, err
	}

	reader := bufio.NewReader(conn)
	length, err := serverReadVarint(reader)
	if err != nil {
		return out, err
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return out, err
	}
	pr := bufio.NewReader(strings.NewReader(string(payload)))
	if _, err := serverReadVarint(pr); err != nil {
		return out, err
	}
	jsonLen, err := serverReadVarint(pr)
	if err != nil {
		return out, err
	}
	raw := make([]byte, jsonLen)
	if _, err := io.ReadFull(pr, raw); err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	return out, nil
}

func serverVarint(v int) []byte {
	var buf [5]byte
	n := 0
	uv := uint32(v)
	for {
		b := byte(uv & 0x7F)
		uv >>= 7
		if uv != 0 {
			b |= 0x80
		}
		buf[n] = b
		n++
		if uv == 0 || n >= len(buf) {
			break
		}
	}
	return buf[:n]
}

func serverString(s string) []byte {
	out := serverVarint(len(s))
	return append(out, []byte(s)...)
}

func serverReadVarint(r *bufio.Reader) (int, error) {
	var value uint32
	for shift := 0; shift < 35; shift += 7 {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		value |= uint32(b&0x7F) << shift
		if b&0x80 == 0 {
			return int(value), nil
		}
	}
	return 0, fmt.Errorf("varint inválido")
}
