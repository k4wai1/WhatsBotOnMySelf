// cita.go porta addons/cita.js: genera una tarjeta de cita con la foto del
// usuario. Sustituye satori/sharp por golang.org/x/image/font.
package addons

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/disintegration/imaging"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

// Variables inyectables en las pruebas.
var (
	citaRand = rand.Float64
	citaHTTP = http.DefaultClient

	citaHopecoreDir = func() string { return core.DataPath("assets", "hopecore") }

	citaProfilePicture = func(ctx *core.Context, pureJID string) ([]byte, error) {
		return citaFetchProfilePicture(ctx, pureJID)
	}

	citaAvatar = func(ctx *core.Context, pureJID, userName string) ([]byte, error) {
		if data, err := citaProfilePicture(ctx, pureJID); err == nil && len(data) > 0 {
			return data, nil
		}
		if data, err := citaRandomHopecore(); err == nil && len(data) > 0 {
			return data, nil
		}
		return citaFetchUIAvatar(userName)
	}
)

var (
	citaFontOnce   sync.Once
	citaFontNormal *opentype.Font
	citaFontBold   *opentype.Font
	citaFontErr    error
)

// Cita porta el comando .cita.
type Cita struct{}

// Commands cumple core.Addon.
func (Cita) Commands() []string { return []string{"cita"} }

// Handle replica el flujo del addon original.
func (c Cita) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	if reactErr := ctx.React("⏳"); reactErr != nil {
		return reactErr
	}

	vertical, horizontal, isColor, cleanArgs := citaParseArgs(ctx.Args)
	if !vertical && !horizontal {
		if citaRand() > 0.5 {
			vertical = true
		} else {
			horizontal = true
		}
	}

	quoteText := strings.TrimSpace(strings.ReplaceAll(strings.Join(cleanArgs, " "), "\n", " "))
	if extraida, ok := citaExtractQuote(quoteText); ok {
		quoteText = extraida
	}

	chatJID := ctx.ChatJID()
	targetID := citaDefaultTarget(ctx, chatJID)

	info := ctx.ContextInfo()
	if mentioned := ctx.MentionedJIDs(); len(mentioned) > 0 {
		targetID = mentioned[0].String()
	} else if info != nil && info.GetQuotedMessage() != nil {
		if info.GetParticipant() != "" {
			targetID = info.GetParticipant()
		} else if chatJID.Server == types.GroupServer {
			targetID = ctx.SenderJID().String()
		} else {
			targetID = chatJID.String()
		}
		if quoteText == "" {
			quoted := ctx.QuotedMessage()
			if quoted != nil {
				qt := quoted.GetConversation()
				if qt == "" && quoted.GetExtendedTextMessage() != nil {
					qt = quoted.GetExtendedTextMessage().GetText()
				}
				quoteText = strings.TrimSpace(strings.ReplaceAll(qt, "\n", " "))
			}
		}
	}

	if quoteText == "" {
		return ctx.Reply("❌ Escribe un texto para citar o responde a un mensaje.")
	}
	if qrJSLength(quoteText) > 560 {
		quoteText = qrTruncate(quoteText, 557) + "..."
	}

	pureJID, userName, phoneNumber := citaResolveUser(ctx, targetID, chatJID.String())

	avatar, err := citaAvatar(ctx, pureJID, userName)
	if err != nil {
		fmt.Printf("❌ [cita] avatar: %v\n", err)
	}

	final, err := citaCompose(quoteText, userName, phoneNumber, avatar, vertical, isColor)
	if err != nil {
		return citaFail(ctx, err)
	}

	if err := ctx.SendImage(final, "📸 *Cita generada*"); err != nil {
		return citaFail(ctx, err)
	}
	return ctx.React("✅")
}

func citaParseArgs(args []string) (vertical, horizontal, isColor bool, clean []string) {
	for _, arg := range args {
		switch strings.ToLower(arg) {
		case "v", "vertical":
			vertical = true
		case "h", "horizontal":
			horizontal = true
		case "c", "color":
			isColor = true
		default:
			clean = append(clean, arg)
		}
	}
	return
}

// citaExtractQuote emula /(["'])(.*?)\1/ (RE2 no soporta backreferences).
func citaExtractQuote(text string) (string, bool) {
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c != '"' && c != '\'' {
			continue
		}
		rest := text[i+1:]
		if j := strings.IndexByte(rest, c); j >= 0 {
			return rest[:j], true
		}
		return "", false
	}
	return "", false
}

func citaDefaultTarget(ctx *core.Context, chatJID types.JID) string {
	if ctx.Message == nil {
		return chatJID.String()
	}
	if chatJID.Server == types.GroupServer {
		return ctx.SenderJID().String()
	}
	if ctx.Message.Info.IsFromMe && ctx.Client != nil && ctx.Client.Store != nil && ctx.Client.Store.ID != nil {
		return ctx.Client.Store.ID.String()
	}
	return chatJID.String()
}

func citaSelfUser(ctx *core.Context) string {
	if ctx.Client != nil && ctx.Client.Store != nil && ctx.Client.Store.ID != nil {
		return ctx.Client.Store.ID.User
	}
	return ""
}

func citaSelfName(ctx *core.Context) string {
	if ctx.Client != nil && ctx.Client.Store != nil {
		return ctx.Client.Store.PushName
	}
	return ""
}

// citaResolveUser replica resolveUser de cita.js.
func citaResolveUser(ctx *core.Context, targetIDRaw, chatJID string) (pureJID, userName, phoneNumber string) {
	targetID := targetIDRaw
	pureJID = ""
	userName = "~"
	phoneNumber = "desconocido"

	if strings.Contains(targetID, "@lid") {
		botLID := ""
		botID := ""
		botName := ""
		if ctx.Client != nil && ctx.Client.Store != nil {
			botLID = ctx.Client.Store.LID.String()
			if ctx.Client.Store.ID != nil {
				botID = ctx.Client.Store.ID.String()
			}
			botName = ctx.Client.Store.PushName
		}
		if botLID != "" && targetID == botLID {
			if botID != "" {
				pureJID = strings.SplitN(botID, ":", 2)[0] + "@s.whatsapp.net"
				phoneNumber = strings.SplitN(pureJID, "@", 2)[0]
				push := ""
				if ctx.Message != nil {
					push = ctx.Message.Info.PushName
				}
				userName = botName
				if userName == "" {
					userName = push
				}
				if userName == "" {
					userName = "Bot"
				}
				return
			}
		}

		if strings.HasSuffix(chatJID, "@g.us") && ctx.Client != nil {
			if jid, err := types.ParseJID(chatJID); err == nil {
				groupCtx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
				meta, err := ctx.Client.GetGroupInfo(groupCtx, jid)
				cancel()
				if err == nil {
					for _, p := range meta.Participants {
						if p.LID.String() == targetID || p.JID.String() == targetID {
							if p.JID.Server == types.DefaultUserServer {
								pureJID = p.JID.String()
								phoneNumber = p.JID.User
								userName = citaContactName(ctx.Store, pureJID, phoneNumber)
								return
							}
						}
					}
				}
			}
		}

		if ctx.Store != nil {
			if contact, ok := ctx.Store.Get(targetID); ok {
				if done := citaResolveLIDContact(contact, targetID, &pureJID, &userName, &phoneNumber); done {
					return
				}
			}
			for key, contact := range ctx.Store.Snapshot() {
				if citaContactMatches(contact, targetID) {
					if done := citaResolveLIDContact(contact, key, &pureJID, &userName, &phoneNumber); done {
						return
					}
					break
				}
			}
		}

		pureJID = targetID
		phoneNumber = strings.SplitN(targetID, "@", 2)[0]
		userName = "Usuario Oculto"
	} else {
		pureJID = targetID
		phoneNumber = strings.SplitN(targetID, "@", 2)[0]
	}

	if pureJID != "" && ctx.Store != nil {
		contact, ok := ctx.Store.Get(pureJID)
		if !ok {
			contact, ok = ctx.Store.Get(targetID)
		}
		if ok {
			for _, key := range []string{"verifiedName", "name", "notify"} {
				if v, ok := contact[key].(string); ok && v != "" {
					userName = v
					break
				}
			}
		}
	}

	if userName == "~" && phoneNumber == citaSelfUser(ctx) {
		push := ""
		if ctx.Message != nil {
			push = ctx.Message.Info.PushName
		}
		userName = push
		if userName == "" {
			userName = citaSelfName(ctx)
		}
		if userName == "" {
			userName = "Luis"
		}
	}

	if userName == "~" {
		if phoneNumber == "desconocido" {
			userName = "~Anónimo~"
		} else {
			userName = phoneNumber
		}
	}
	return
}

func citaContactMatches(contact map[string]any, targetID string) bool {
	if v, ok := contact["lid"].(string); ok && v == targetID {
		return true
	}
	if v, ok := contact["id"].(string); ok && v == targetID {
		return true
	}
	return false
}

func citaResolveLIDContact(contact map[string]any, key string, pureJID, userName, phoneNumber *string) bool {
	realNumber := ""
	if v, ok := contact["pn"].(string); ok {
		realNumber = v
	}
	if realNumber == "" && strings.HasSuffix(key, "@s.whatsapp.net") {
		realNumber = strings.SplitN(key, "@", 2)[0]
	}
	if realNumber == "" {
		return false
	}
	*pureJID = realNumber + "@s.whatsapp.net"
	*phoneNumber = realNumber
	*userName = realNumber
	for _, k := range []string{"verifiedName", "name", "notify"} {
		if v, ok := contact[k].(string); ok && v != "" {
			*userName = v
			break
		}
	}
	return true
}

func citaContactName(st *store.Store, pureJID, fallback string) string {
	if st == nil {
		return fallback
	}
	if contact, ok := st.Get(pureJID); ok {
		for _, key := range []string{"verifiedName", "name", "notify"} {
			if v, ok := contact[key].(string); ok && v != "" {
				return v
			}
		}
	}
	return fallback
}

func citaFetchProfilePicture(ctx *core.Context, pureJID string) ([]byte, error) {
	if ctx.Client == nil || pureJID == "" {
		return nil, fmt.Errorf("sin cliente")
	}
	jid, err := types.ParseJID(pureJID)
	if err != nil {
		return nil, err
	}
	pctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	info, err := ctx.Client.GetProfilePictureInfo(pctx, jid, &whatsmeow.GetProfilePictureParams{Preview: false})
	if err != nil || info == nil || info.URL == "" {
		info, err = ctx.Client.GetProfilePictureInfo(pctx, jid, &whatsmeow.GetProfilePictureParams{Preview: true})
	}
	if err != nil || info == nil || info.URL == "" {
		return nil, fmt.Errorf("sin foto de perfil")
	}

	req, err := http.NewRequestWithContext(pctx, http.MethodGet, info.URL, nil)
	if err != nil {
		return nil, err
	}
	res, err := citaHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return io.ReadAll(res.Body)
}

func citaRandomHopecore() ([]byte, error) {
	entries, err := os.ReadDir(citaHopecoreDir())
	if err != nil {
		return nil, err
	}
	valid := regexp.MustCompile(`(?i)\.(jpg|jpeg|png)$`)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && valid.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("sin imágenes hopecore")
	}
	idx := int(citaRand() * float64(len(names)))
	if idx >= len(names) || idx < 0 {
		idx = 0
	}
	return os.ReadFile(filepath.Join(citaHopecoreDir(), names[idx]))
}

func citaFetchUIAvatar(userName string) ([]byte, error) {
	avatarURL := "https://ui-avatars.com/api/?name=" + citaEncodeURIComponent(userName) + "&background=random&color=fff&size=512"
	req, err := http.NewRequest(http.MethodGet, avatarURL, nil)
	if err != nil {
		return nil, err
	}
	res, err := citaHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return io.ReadAll(res.Body)
}

// citaEncodeURIComponent replica encodeURIComponent de JavaScript.
func citaEncodeURIComponent(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9',
			strings.ContainsRune("-_.!~*'()", r):
			b.WriteRune(r)
		default:
			for _, part := range []byte(string(r)) {
				fmt.Fprintf(&b, "%%%02X", part)
			}
		}
	}
	return b.String()
}

func citaLoadFonts() (*opentype.Font, *opentype.Font, error) {
	citaFontOnce.Do(func() {
		citaFontNormal, citaFontErr = opentype.Parse(goregular.TTF)
		if citaFontErr != nil {
			return
		}
		citaFontBold, citaFontErr = opentype.Parse(gobold.TTF)
	})
	return citaFontNormal, citaFontBold, citaFontErr
}

func citaOptimalFontSize(text string, width, height, maxFontSize int) int {
	textLength := qrJSLength(text)
	if textLength < 1 {
		textLength = 1
	}
	boxArea := float64(width * height)
	targetArea := boxArea * 0.70
	calculated := int(math.Floor(math.Sqrt(targetArea / (float64(textLength) * 0.55))))
	if calculated > maxFontSize {
		return maxFontSize
	}
	if calculated < 8 {
		return 8
	}
	return calculated
}

type citaWord struct {
	text string
	bold bool
}

type citaFaceCache struct {
	normal *opentype.Font
	bold   *opentype.Font
	faces  map[[2]int]font.Face
}

func (c *citaFaceCache) face(bold bool, size int) (font.Face, error) {
	key := [2]int{size, 0}
	if bold {
		key[1] = 1
	}
	if f, ok := c.faces[key]; ok {
		return f, nil
	}
	src := c.normal
	if bold {
		src = c.bold
	}
	f, err := opentype.NewFace(src, &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil, err
	}
	c.faces[key] = f
	return f, nil
}

// citaCompose dibuja la tarjeta final.
func citaCompose(quoteText, userName, phoneNumber string, avatar []byte, vertical, isColor bool) ([]byte, error) {
	normal, bold, err := citaLoadFonts()
	if err != nil {
		return nil, err
	}
	if len(avatar) == 0 {
		return nil, fmt.Errorf("avatar vacío")
	}
	avatarImg, _, err := image.Decode(bytes.NewReader(avatar))
	if err != nil {
		return nil, err
	}
	if !isColor {
		avatarImg = citaGrayscale(avatarImg)
	}

	cache := &citaFaceCache{normal: normal, bold: bold, faces: make(map[[2]int]font.Face)}
	words := citaMakeWords(quoteText)

	if vertical {
		return citaComposeVertical(cache, avatarImg, words, userName, phoneNumber)
	}
	return citaComposeHorizontal(cache, avatarImg, words, userName, phoneNumber)
}

func citaMakeWords(quoteText string) []citaWord {
	parts := strings.Fields("“" + quoteText + "”")
	words := make([]citaWord, 0, len(parts))
	for _, part := range parts {
		words = append(words, citaWord{text: part, bold: citaRand() < 0.25})
	}
	return words
}

func citaComposeVertical(cache *citaFaceCache, avatar image.Image, words []citaWord, userName, phoneNumber string) ([]byte, error) {
	const width, height = 720, 960
	canvas := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.Black}, image.Point{}, draw.Src)

	avatarLayer := imaging.Fill(avatar, 720, 720, imaging.Center, imaging.Lanczos)
	draw.Draw(canvas, image.Rect(0, 0, 720, 720), avatarLayer, avatarLayer.Bounds().Min, draw.Over)

	for y := 576; y < 720; y++ {
		alpha := float64(y-576) / 144.0
		citaBlendPixel(canvas, 0, y, 720, color.NRGBA{0, 0, 0, uint8(math.Round(255 * alpha))})
	}
	for y := 720; y < height; y++ {
		citaBlendPixel(canvas, 0, y, width, color.NRGBA{0, 0, 0, 255})
	}

	fontSize := citaOptimalFontSize(strings.Join(citaWordTexts(words), " "), 640, 330, 60)
	if err := citaDrawBlock(canvas, cache, words, fontSize, 40, 520, 640, 330); err != nil {
		return nil, err
	}
	citaDrawUserName(canvas, cache, userName, phoneNumber, 0, 880, 720, false)
	return citaEncodePNG(canvas)
}

func citaComposeHorizontal(cache *citaFaceCache, avatar image.Image, words []citaWord, userName, phoneNumber string) ([]byte, error) {
	const width, height = 1280, 720
	canvas := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.Black}, image.Point{}, draw.Src)

	avatarLayer := imaging.Fill(avatar, 620, 620, imaging.Center, imaging.Lanczos)
	citaRoundCorners(avatarLayer, 31)
	for x := 496; x < 620; x++ {
		alpha := float64(x-496) / 124.0
		citaBlendPixelRect(avatarLayer, x, 0, 1, 620, color.NRGBA{0, 0, 0, uint8(math.Round(255 * alpha))})
	}
	draw.Draw(canvas, image.Rect(50, 50, 670, 670), avatarLayer, avatarLayer.Bounds().Min, draw.Over)

	fontSize := citaOptimalFontSize(strings.Join(citaWordTexts(words), " "), 630, 440, 60)
	if err := citaDrawBlock(canvas, cache, words, fontSize, 600, 100, 630, 440); err != nil {
		return nil, err
	}
	citaDrawUserName(canvas, cache, userName, phoneNumber, 600, 560, 630, false)
	return citaEncodePNG(canvas)
}

func citaWordTexts(words []citaWord) []string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		out = append(out, w.text)
	}
	return out
}

// citaDrawBlock dibuja las palabras centradas y con salto de línea.
func citaDrawBlock(dst *image.NRGBA, cache *citaFaceCache, words []citaWord, fontSize, left, top, width, height int) error {
	if len(words) == 0 {
		return nil
	}
	lineHeight := int(math.Round(float64(fontSize) * 1.25))

	type laidWord struct {
		text string
		face font.Face
		w    int
	}
	lines := make([][]laidWord, 0, 8)
	current := make([]laidWord, 0, 8)
	currentWidth := 0

	for _, word := range words {
		face, err := cache.face(word.bold, fontSize)
		if err != nil {
			return err
		}
		w := int(font.MeasureString(face, word.text).Round()) + 8
		if len(current) > 0 && currentWidth+w-8 > width {
			lines = append(lines, current)
			current = make([]laidWord, 0, 8)
			currentWidth = 0
		}
		current = append(current, laidWord{text: word.text, face: face, w: w})
		currentWidth += w
	}
	if len(current) > 0 {
		lines = append(lines, current)
	}

	totalHeight := len(lines) * lineHeight
	startY := top + (height-totalHeight)/2
	white := &image.Uniform{C: color.White}
	shadow := &image.Uniform{C: color.NRGBA{0, 0, 0, 230}}

	for i, line := range lines {
		lineWidth := 0
		for _, word := range line {
			lineWidth += word.w
		}
		x := left + (width-lineWidth)/2
		ascent := fontSize
		baseline := startY + i*lineHeight + ascent
		for _, word := range line {
			citaDrawText(dst, shadow, word.face, x+2, baseline+2, word.text)
			citaDrawText(dst, white, word.face, x, baseline, word.text)
			x += word.w
		}
	}
	return nil
}

func citaDrawUserName(dst *image.NRGBA, cache *citaFaceCache, userName, phoneNumber string, left, top, width int, _ bool) {
	faceName, err := cache.face(false, 28)
	if err == nil {
		nameWidth := int(font.MeasureString(faceName, userName).Round())
		x := left + (width-nameWidth)/2
		citaDrawText(dst, &image.Uniform{C: color.NRGBA{0, 0, 0, 204}}, faceName, x+1, top+29, userName)
		citaDrawText(dst, &image.Uniform{C: color.NRGBA{238, 238, 238, 255}}, faceName, x, top+28, userName)
	}
	facePhone, err := cache.face(false, 20)
	if err == nil {
		phoneWidth := int(font.MeasureString(facePhone, phoneNumber).Round())
		x := left + (width-phoneWidth)/2
		citaDrawText(dst, &image.Uniform{C: color.NRGBA{255, 255, 255, 153}}, facePhone, x, top+52, phoneNumber)
	}
}

func citaDrawText(dst draw.Image, src image.Image, face font.Face, x, baseline int, text string) {
	d := font.Drawer{
		Dst:  dst,
		Src:  src,
		Face: face,
		Dot:  fixed.P(x, baseline),
	}
	d.DrawString(text)
}

func citaBlendPixel(img *image.NRGBA, x, y, width int, c color.NRGBA) {
	for i := x; i < x+width; i++ {
		img.SetNRGBA(i, y, citaBlend(img.NRGBAAt(i, y), c))
	}
}

func citaBlendPixelRect(img *image.NRGBA, x, y, w, h int, c color.NRGBA) {
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			img.SetNRGBA(xx, yy, citaBlend(img.NRGBAAt(xx, yy), c))
		}
	}
}

func citaBlend(dst, src color.NRGBA) color.NRGBA {
	a := float64(src.A) / 255
	return color.NRGBA{
		R: uint8(float64(src.R)*a + float64(dst.R)*(1-a)),
		G: uint8(float64(src.G)*a + float64(dst.G)*(1-a)),
		B: uint8(float64(src.B)*a + float64(dst.B)*(1-a)),
		A: 255,
	}
}

func citaGrayscale(src image.Image) image.Image {
	img := imaging.Clone(src)
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.NRGBAAt(x, y)
			lum := uint8(0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B))
			img.SetNRGBA(x, y, color.NRGBA{lum, lum, lum, c.A})
		}
	}
	return img
}

func citaRoundCorners(img *image.NRGBA, radius int) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	inside := func(x, y int) bool {
		if x < radius && y < radius {
			return citaDist(x-radius, y-radius) <= float64(radius)
		}
		if x >= w-radius && y < radius {
			return citaDist(x-(w-radius-1), y-radius) <= float64(radius)
		}
		if x < radius && y >= h-radius {
			return citaDist(x-radius, y-(h-radius-1)) <= float64(radius)
		}
		if x >= w-radius && y >= h-radius {
			return citaDist(x-(w-radius-1), y-(h-radius-1)) <= float64(radius)
		}
		return true
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if !inside(x, y) {
				img.SetNRGBA(x, y, color.NRGBA{})
			}
		}
	}
}

func citaDist(dx, dy int) float64 {
	return math.Sqrt(float64(dx*dx + dy*dy))
}

func citaEncodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func citaFail(ctx *core.Context, err error) error {
	fmt.Printf("❌ [cita]: %v\n", err)
	if reactErr := ctx.React("❌"); reactErr != nil {
		return reactErr
	}
	return nil
}

func init() { core.Register(Cita{}) }
