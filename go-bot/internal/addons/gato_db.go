// gato_db.go porta addons/gato/db.js: mini base de datos de jugadores,
// decaimiento, racha y economía.
package addons

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"whatsbot-go/internal/core"
)

const (
	gatoDecayHambre     = 4.0
	gatoDecayFelicidad  = 3.0
	gatoDecayEnergia    = 2.0
	gatoMultPorDiaRacha = 0.05
	gatoMultRachaMax    = 2.0
)

// gatoNow es inyectable para usar un reloj falso en pruebas.
var gatoNow = time.Now

// gatoDBPath es inyectable en pruebas.
var gatoDBPath = func() string { return core.DataPath("assets", "gatos_data.json") }

type gatoStats struct {
	Hambre    float64 `json:"hambre"`
	Energia   float64 `json:"energia"`
	Felicidad float64 `json:"felicidad"`
}

type gatoEconomia struct {
	Peces int `json:"peces"`
	XP    int `json:"xp"`
	Nivel int `json:"nivel"`
}

type gatoRacha struct {
	Dias      int     `json:"dias"`
	UltimoDia *string `json:"ultimoDia"`
}

type gatoDiario struct {
	Fecha   *string `json:"fecha"`
	Comida  bool    `json:"comida,omitempty"`
	Caricia bool    `json:"caricia,omitempty"`
	Banio   bool    `json:"baño,omitempty"`
}

type gatoSiesta struct {
	Inicio    int64 `json:"inicio"`
	Despierta int64 `json:"despierta"`
}

type gatoCat struct {
	ID             string        `json:"id,omitempty"`
	Name           string        `json:"name"`
	Scene          string        `json:"scene"`
	CreatedAt      int64         `json:"createdAt"`
	LastUpdate     int64         `json:"lastUpdate"`
	Stats          *gatoStats    `json:"stats"`
	Economia       *gatoEconomia `json:"economia"`
	Racha          *gatoRacha    `json:"racha"`
	Diario         *gatoDiario   `json:"diario"`
	Siesta         *gatoSiesta   `json:"siesta"`
	UltimaCaricia  int64         `json:"ultimaCaricia"`
	UltimaBanio    int64         `json:"ultimaBanio"`
}

type gatoGanado struct {
	XP    int
	Peces int
	Subio bool
}

func gatoClamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// gatoCargar lee el JSON y normaliza todas las entradas.
func gatoCargar() map[string]*gatoCat {
	db := make(map[string]*gatoCat)
	raw, err := os.ReadFile(gatoDBPath())
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Printf("Error leyendo gatos_data: %v\n", err)
		}
		return db
	}
	if len(raw) == 0 {
		return db
	}
	if err := json.Unmarshal(raw, &db); err != nil {
		fmt.Printf("Error leyendo gatos_data: %v\n", err)
		return make(map[string]*gatoCat)
	}
	if db == nil {
		db = make(map[string]*gatoCat)
	}
	for _, cat := range db {
		if cat != nil {
			gatoNormalizar(cat)
		}
	}
	return db
}

// gatoGuardar escribe con la misma indentación de JSON.stringify(db, null, 2).
func gatoGuardar(db map[string]*gatoCat) {
	raw, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		fmt.Printf("Error guardando gatos_data: %v\n", err)
		return
	}
	path := gatoDBPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Printf("Error guardando gatos_data: %v\n", err)
		return
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		fmt.Printf("Error guardando gatos_data: %v\n", err)
	}
}

func gatoCrear(id string) *gatoCat {
	now := gatoNow().UnixMilli()
	return &gatoCat{
		ID:         id,
		Name:       "Michi",
		Scene:      "Sala Principal",
		CreatedAt:  now,
		LastUpdate: now,
		Stats:      &gatoStats{Hambre: 50, Energia: 100, Felicidad: 50},
		Economia:   &gatoEconomia{Peces: 0, XP: 0, Nivel: 1},
		Racha:      &gatoRacha{Dias: 0},
		Diario:     &gatoDiario{},
	}
}

func gatoNormalizar(cat *gatoCat) {
	now := gatoNow().UnixMilli()
	if cat.Name == "" {
		cat.Name = "Michi"
	}
	if cat.Scene == "" {
		cat.Scene = "Sala Principal"
	}
	if cat.CreatedAt == 0 {
		cat.CreatedAt = now
	}
	if cat.LastUpdate == 0 {
		cat.LastUpdate = now
	}
	if cat.Stats == nil {
		cat.Stats = &gatoStats{Hambre: 50, Energia: 100, Felicidad: 50}
	}
	cat.Stats.Hambre = gatoClamp(cat.Stats.Hambre)
	cat.Stats.Energia = gatoClamp(cat.Stats.Energia)
	cat.Stats.Felicidad = gatoClamp(cat.Stats.Felicidad)
	if cat.Economia == nil {
		cat.Economia = &gatoEconomia{Nivel: 1}
	}
	if cat.Racha == nil {
		cat.Racha = &gatoRacha{}
	}
	if cat.Diario == nil {
		cat.Diario = &gatoDiario{}
	}
}

func gatoHoyISO() string {
	return gatoNow().UTC().Format("2006-01-02")
}

func gatoAyerISO() string {
	return gatoNow().Add(-24 * time.Hour).UTC().Format("2006-01-02")
}

// gatoAplicarDecay replica aplicarDecay de db.js.
func gatoAplicarDecay(cat *gatoCat) {
	ahora := gatoNow().UnixMilli()
	horas := float64(ahora-cat.LastUpdate) / 3600000.0
	if horas < 0 {
		horas = 0
	}
	if horas <= 0 {
		return
	}
	if cat.Siesta != nil && cat.Siesta.Despierta > ahora {
		cat.Stats.Hambre = gatoClamp(cat.Stats.Hambre - gatoDecayHambre*horas*0.5)
	} else {
		cat.Stats.Hambre = gatoClamp(cat.Stats.Hambre - gatoDecayHambre*horas)
		cat.Stats.Felicidad = gatoClamp(cat.Stats.Felicidad - gatoDecayFelicidad*horas)
		cat.Stats.Energia = gatoClamp(cat.Stats.Energia - gatoDecayEnergia*horas)
	}
	cat.LastUpdate = ahora
}

func gatoRegistrarRacha(cat *gatoCat) float64 {
	hoy := gatoHoyISO()
	ayer := gatoAyerISO()
	if cat.Racha.UltimoDia != nil {
		switch *cat.Racha.UltimoDia {
		case hoy:
			// ya contó hoy
		case ayer:
			cat.Racha.Dias++
		default:
			cat.Racha.Dias = 1
		}
	} else {
		cat.Racha.Dias = 1
	}
	h := hoy
	cat.Racha.UltimoDia = &h
	return gatoMultiplicador(cat)
}

func gatoMultiplicador(cat *gatoCat) float64 {
	dias := 0
	if cat.Racha != nil {
		dias = cat.Racha.Dias
	}
	m := 1 + gatoMultPorDiaRacha*float64(dias)
	if m > gatoMultRachaMax {
		return gatoMultRachaMax
	}
	return m
}

// gatoMarcarDiario devuelve true la primera vez del día.
func gatoMarcarDiario(cat *gatoCat, tipo string) bool {
	hoy := gatoHoyISO()
	if cat.Diario.Fecha == nil || *cat.Diario.Fecha != hoy {
		h := hoy
		cat.Diario = &gatoDiario{Fecha: &h}
	}
	switch tipo {
	case "comida":
		if cat.Diario.Comida {
			return false
		}
		cat.Diario.Comida = true
	case "caricia":
		if cat.Diario.Caricia {
			return false
		}
		cat.Diario.Caricia = true
	case "baño":
		if cat.Diario.Banio {
			return false
		}
		cat.Diario.Banio = true
	default:
		return false
	}
	return true
}

func gatoOtorgar(cat *gatoCat, xp, peces int) gatoGanado {
	ganado := gatoGanado{XP: int(math.Round(float64(xp) * gatoMultiplicador(cat))), Peces: peces}
	cat.Economia.XP += ganado.XP
	cat.Economia.Peces += peces
	for cat.Economia.XP >= cat.Economia.Nivel*100 {
		cat.Economia.XP -= cat.Economia.Nivel * 100
		cat.Economia.Nivel++
		ganado.Subio = true
	}
	return ganado
}

func gatoAnimo(cat *gatoCat) string {
	s := cat.Stats
	if s.Hambre < 25 {
		return "hambriento"
	}
	if s.Felicidad < 25 {
		return "triste"
	}
	if s.Felicidad >= 85 {
		return "feliz"
	}
	return ""
}
