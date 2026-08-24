<div align="center">

# 🤖 WhatsBotOnMySelf

**Self-bot personal de WhatsApp — modular, silencioso y extensible**  
**Personal WhatsApp self-bot — modular, silent and extensible**

<img src="https://count.getloli.com/@Kira?name=Kira&theme=booru-lewd&padding=7&offset=0&align=center&scale=1&pixelated=1&darkmode=auto" alt="Moe Counter" />

[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)
![Node](https://img.shields.io/badge/node-%3E%3D18.0-brightgreen)
![pnpm](https://img.shields.io/badge/pnpm-%3E%3D11.5.1-orange)

---

</div>

## 📑 Table of Contents / Índice

- [English](#-english)
- [Español](#-español)

---

## 🇬🇧 English

### What is this?

**WhatsBotOnMySelf** is a WhatsApp self-bot written in **Node.js** using the [`@whiskeysockets/baileys`](https://github.com/WhiskeySockets/Baileys) library. It runs on your own WhatsApp account (not a bot API), allowing you to automate tasks, create quick commands, and add quality-of-life tools to your daily chat experience.

> ⚠️ **Disclaimer:** This project is intended for **personal and educational use only**. Using self-bots may violate WhatsApp's Terms of Service. Use at your own risk.

### Features

- **Modular architecture** — Every command is an independent addon inside the `addons/` folder. Add, remove or disable features without touching the core.
- **Silent interface (Zero-Spam)** — Reactions (emojis) on the user's message indicate status instead of spamming the chat with text.
- **Dynamic command loading** — Addons are loaded on startup. No need to edit `index.js` to add new commands.
- **Custom store** — Lightweight in-memory contact store that persists to disk (replaces the deprecated `makeInMemoryStore`).
- **Auto-reconnect** — Automatically reconnects if the connection drops (unless logged out).

### Addons included

| Addon | Description |
|---|---|
| `stickers` | Convert images/videos/GIFs to stickers + `.st a` collection mode (send multiple files, each becomes a sticker) |
| `trans` | Translate text messages |
| `acortar` | Shorten long links with is.gd (`.acortar <url>`, also works quoting a message) |
| `cita` | Generate beautiful quote images |
| `chat` | Agentic AI chat (DeepSeek): quoted-message context, chat history with XML delimiters, image understanding (`deepseek-v4-flash-vision-exp`) and web search via You.com + **Parallel Search MCP** (up to 5 tool rounds, forced final answer) |
| `ytdl` | YouTube / TikTok / X(Twitter) downloader with interactive format menu, search and ZIP mode. Self-updates yt-dlp daily |
| `resume` | Transcribe + summarize voice notes AND videos (Groq Whisper + DeepSeek) |
| `fetch` | Download media from URLs |
| `files` | Browse the server's `files/` folder from the chat (paginated interactive sessions) |
| `paste` | Upload typed or quoted text to paste.rs and get a permanent link (0x0.st fallback) |
| `emoji-to-gif` | Convert emojis to animated GIFs |
| `fx` | Image effects on quoted photos/stickers: `.fx blur\|gris\|sepia\|espejo` |
| `audio_editor` | Audio editing (convert, trim, waveform); accepts videos too (extracts audio) |
| `github` | Sends the project repo link (`.git`, `.github`, `.repo`) |
| `perfil` | View/profile management |
| `prefix` | View/change the bot's command prefixes at runtime |
| `tasas` | Exchange rates & currency conversion |
| `wallpaper` | Random wallpaper by keyword from wallhaven.cc (auto-compressed JPEG) |
| `cordial` | Replaces rude words in a quoted audio with polite TTS |
| `bal` | DeepSeek API balance checker |
| `help` | Auto-generated help menu (uses AI to catalog commands, auto-refreshes when addons change) |
| `ft` | Sticker → photo converter |
| `debug-info` / `debug-pp` | Debugging tools |
| `misc` | Misc commands: `.ping` (WhatsApp server latency), `.pong` (bot reaction time, self-edits with the ms) |
| `dado` | Dice roller `.dado [N]` (N-sided die) and coin flip `.moneda` |
| `rem` | Persistent reminders: `.rem 30m text`, `.rem 1h45m x`, `.rem 17:30`, `.rem mañana 8am gym`, list/del/clear |
| `voz` | Text → natural voice note via edge-tts (`.voz texto`, `.voz @es-MX-DaliaNeural texto`, `.voz voces`) |
| `qr` | QR generator + reader: `.qr <text>` creates one; reply to an image with `.qr` to decode it |
| `ss` | Website screenshot: `.ss example.com` → PNG (Chrome headless or lightpanda fallback) |
| `looger` | Colored console message logger (background) |
| `unread` | Catch-up summary of missed messages (background history) |
| `img` | AI vision: describe/analyze a photo sent or quoted in the chat |

### Requirements

- **Node.js** v18 or higher
- **pnpm** (package manager)
- **FFmpeg** (for audio/video processing)
- A **Debian-based system** (recommended), **Termux** (Android), or any Linux distro

### Installation

#### 1. Install dependencies (Debian / Ubuntu / Linux Mint)

```bash
sudo apt update
sudo apt install -y git ffmpeg curl nodejs npm
```

> If your distro ships an old Node.js version, use [NodeSource](https://github.com/nodesource/distributions) to get v18+.

#### 2. Install pnpm (recommended on Linux)

```bash
curl -fsSL https://get.pnpm.io/install.sh | sh -
source ~/.bashrc
```

> If you're on **Termux**, use `npm` instead — it's more compatible there:
> ```bash
> npm install -g pnpm
> ```

#### 3. Clone & setup

```bash
git clone https://github.com/<YOUR_USER>/WhatsBotOnMySelf.git
cd WhatsBotOnMySelf
pnpm install
```

#### 4. Run

```bash
node index.js
```

Scan the QR code with your WhatsApp linked devices.

#### Optional: `.env` file

Create a `.env` file in the root directory if any addon requires API keys:

```env
# Example
DEEPSEEK_API_KEY=your_key_here
YOU_API_KEY=your_key_here

# Optional — Parallel Search MCP works without a key (free tier).
# Only add it for higher rate limits: https://platform.parallel.ai
PARALLEL_API_KEY=
```

### Project structure

```
WhatsBotOnMySelf/
├── index.js              # Core — connection, routing, loader
├── addons/               # Independent command modules
│   ├── stickers.js
│   ├── trans.js
│   ├── cita.js
│   └── ...
├── assets/               # Static resources (fonts, images, etc.)
│   └── fonts/
├── package.json
├── .gitignore
└── README.md
```

### Creating your own addon

Create a file in `addons/` (e.g., `ping.js`):

```js
// addons/ping.js

const commands = ['ping'];

async function handler(sock, msg, args) {
    const start = Date.now();
    await sock.sendMessage(msg.key.remoteJid, { react: { text: '⏳', key: msg.key } });
    const latency = Date.now() - start;
    await sock.sendMessage(msg.key.remoteJid, { text: `🏓 Pong! ${latency}ms` });
}

module.exports = { commands, handler };
```

---

## 🇪🇸 Español

### ¿Qué es esto?

**WhatsBotOnMySelf** es un self-bot de WhatsApp escrito en **Node.js** que usa la librería [`@whiskeysockets/baileys`](https://github.com/WhiskeySockets/Baileys). Funciona con tu cuenta personal de WhatsApp (no es una API de bot), permitiéndote automatizar tareas, crear comandos rápidos y añadir herramientas de calidad de vida a tu experiencia diaria en el chat.

> ⚠️ **Aviso:** Este proyecto es solo para **uso personal y educativo**. El uso de self-bots puede violar los Términos de Servicio de WhatsApp. Úsalo bajo tu propia responsabilidad.

### Características

- **Arquitectura modular** — Cada comando es un addon independiente dentro de la carpeta `addons/`. Añade, elimina o desactiva funcionalidades sin tocar el núcleo.
- **Interfaz silenciosa (Zero-Spam)** — Se usan reacciones (emojis) sobre el mensaje del usuario para indicar estado, evitando enviar textos innecesarios.
- **Carga dinámica de comandos** — Los addons se cargan al iniciar. No necesitas editar `index.js` para añadir nuevos comandos.
- **Almacén personalizado** — Almacén de contactos ligero en memoria con persistencia en disco (reemplaza el obsoleto `makeInMemoryStore`).
- **Reconexión automática** — Se reconecta automáticamente si la conexión se cae (a menos que sea un cierre de sesión).

### Addons incluidos

| Addon | Descripción |
|---|---|
| `stickers` | Convertir imágenes/videos/GIFs a stickers + modo colección `.st a` (envía varios archivos y cada uno se vuelve sticker) |
| `trans` | Traducir mensajes de texto |
| `acortar` | Acorta enlaces largos con is.gd (`.acortar <url>`, también funciona citando un mensaje) |
| `cita` | Generar imágenes de citas con diseño elegante |
| `chat` | Chat IA agéntico (DeepSeek): entiende mensajes citados, historial del chat con delimitadores XML, lee imágenes (`deepseek-v4-flash-vision-exp`) y busca en la web vía You.com + **Parallel Search MCP** (hasta 5 rondas de herramientas, respuesta final obligatoria) |
| `ytdl` | Descargador YouTube / TikTok / X(Twitter) con menú interactivo, búsqueda y modo ZIP. Auto-actualiza yt-dlp a diario |
| `resume` | Transcribe y resume notas de voz Y videos (Groq Whisper + DeepSeek) |
| `fetch` | Descargar contenido multimedia desde URLs |
| `files` | Explora la carpeta `files/` del servidor desde el chat (sesiones interactivas paginadas) |
| `paste` | Sube texto escrito o citado a paste.rs y obtén un link permanente (respaldo 0x0.st) |
| `emoji-to-gif` | Convertir emojis a GIFs animados |
| `fx` | Efectos de imagen sobre fotos/stickers citados: `.fx blur\|gris\|sepia\|espejo` |
| `audio_editor` | Edición de audio (convertir, recortar, waveform); también acepta videos (extrae el audio) |
| `github` | Envía el link del repo (`.git`, `.github`, `.repo`) |
| `perfil` | Gestión de perfil e información |
| `prefix` | Ver/cambiar los prefijos de comandos del bot en caliente |
| `tasas` | Tasas de cambio y conversión de divisas |
| `wallpaper` | Wallpaper aleatorio por palabra clave desde wallhaven.cc (comprime a JPEG automáticamente) |
| `cordial` | Reemplaza groserías de un audio citado con TTS cordial sincronizado |
| `bal` | Consulta el saldo de la API de DeepSeek |
| `help` | Menú de ayuda autogenerado (usa IA para catalogar comandos, se refresca solo cuando cambian los addons) |
| `ft` | Convierte stickers citados de vuelta a foto |
| `debug-info` / `debug-pp` | Herramientas de depuración |
| `misc` | Comandos misceláneos: `.ping` (latencia con servidores de WhatsApp), `.pong` (tiempo de reacción del bot, se auto-edita con los ms) |
| `dado` | Dados `.dado [N]` (dado de N caras) y moneda `.moneda` |
| `rem` | Recordatorios persistentes: `.rem 30m texto`, `.rem 1h45m x`, `.rem 17:30`, `.rem mañana 8am gym`, con list/del/clear |
| `voz` | Texto → nota de voz natural con edge-tts (`.voz texto`, `.voz @es-MX-DaliaNeural texto`, `.voz voces`) |
| `qr` | Generador y lector de QR: `.qr <texto>` crea uno; responde a una imagen con `.qr` para leer su contenido |
| `ss` | Captura de páginas web: `.ss example.com` → PNG (Chrome headless o lightpanda como respaldo) |
| `looger` | Log colorido de mensajes en consola (segundo plano) |
| `unread` | Resumen de lo que te perdiste (historial en segundo plano) |
| `img` | Visión IA: describe/analiza una foto enviada o citada en el chat |

### Requisitos

- **Node.js** v18 o superior
- **pnpm** (gestor de paquetes)
- **FFmpeg** (para procesamiento de audio/video)
- Un sistema **basado en Debian** (recomendado), **Termux** (Android), o cualquier distro Linux

### Instalación

#### 1. Instalar dependencias (Debian / Ubuntu / Linux Mint)

```bash
sudo apt update
sudo apt install -y git ffmpeg curl nodejs npm
```

> Si tu distro incluye una versión antigua de Node.js, usa [NodeSource](https://github.com/nodesource/distributions) para obtener v18+.

#### 2. Instalar pnpm (recomendado en Linux)

```bash
curl -fsSL https://get.pnpm.io/install.sh | sh -
source ~/.bashrc
```

> Si usas **Termux**, es mejor usar `npm` — es más compatible allí:
> ```bash
> npm install -g pnpm
> ```

#### 3. Clonar y configurar

```bash
git clone https://github.com/<TU_USUARIO>/WhatsBotOnMySelf.git
cd WhatsBotOnMySelf
pnpm install
```

#### 4. Ejecutar

```bash
node index.js
```

Escanea el código QR con tus dispositivos vinculados de WhatsApp.

#### Opcional: archivo `.env`

Crea un archivo `.env` en la raíz si algún addon requiere claves de API:

```env
# Ejemplo
DEEPSEEK_API_KEY=tu_clave_aqui
YOU_API_KEY=tu_clave_aqui

# Opcional — Parallel Search MCP funciona sin key (gratis con límites bajos).
# Solo agrégala si quieres límites más altos: https://platform.parallel.ai
PARALLEL_API_KEY=
```

### Estructura del proyecto

```
WhatsBotOnMySelf/
├── index.js              # Núcleo — conexión, enrutamiento, cargador
├── addons/               # Módulos de comando independientes
│   ├── stickers.js
│   ├── trans.js
│   ├── cita.js
│   └── ...
├── assets/               # Recursos estáticos (fuentes, imágenes, etc.)
│   └── fonts/
├── package.json
├── .gitignore
└── README.md
```

### Crear tu propio addon

Crea un archivo en `addons/` (ej. `ping.js`):

```js
// addons/ping.js

const commands = ['ping'];

async function handler(sock, msg, args) {
    const start = Date.now();
    await sock.sendMessage(msg.key.remoteJid, { react: { text: '⏳', key: msg.key } });
    const latency = Date.now() - start;
    await sock.sendMessage(msg.key.remoteJid, { text: `🏓 Pong! ${latency}ms` });
}

module.exports = { commands, handler };
```

---

## 🙏 Credits / Créditos

This project stands on the shoulders of these awesome open-source projects /  
Este proyecto se sostiene sobre los hombros de estos increíbles proyectos de código abierto:

- [**Baileys**](https://github.com/WhiskeySockets/Baileys) — WhatsApp Web API implementation for Node.js (@whiskeysockets/baileys)
- [**Sharp**](https://sharp.pixelplumbing.com/) — High-performance image processing
- [**Satori**](https://github.com/vercel/satori) — Enlightened library to convert HTML and CSS to SVG
- [**Canvas (node-canvas)**](https://github.com/Automattic/node-canvas) — Cairo-backed Canvas implementation
- [**Lightpanda Browser**](https://lightpanda.io/) — Lightweight browser for automation
- [**fluent-ffmpeg**](https://github.com/fluent-ffmpeg/node-fluent-ffmpeg) — FFmpeg wrapper for Node.js
- [**Pino**](https://getpino.io/) — Super fast Node.js logger
- [**Moe Counter**](https://count.getloli.com/) — Awesome anime-style visit counters

---

## 📄 License / Licencia

```
Copyright (C) 2026 kwai1

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
```

The following files are original work of the author and licensed under **GPL v3**:
- `index.js`
- All files inside `addons/`

All other dependencies retain their original licenses.

---

<div align="center">
  <br><br>
  <a href="https://ko-fi.com/kwai1">
    <img src="https://ko-fi.com/img/githubbutton_sm.svg" alt="Support me on Ko-fi" />
  </a>
</div>
