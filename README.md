<div align="center">

# 🤖 WhatsBotOnMySelf

**Tu WhatsApp personal, con superpoderes.**  
Self-bot de WhatsApp escrito en **Go** con [`whatsmeow`](https://github.com/whatsmeow/whatsmeow).

![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go)
![License](https://img.shields.io/badge/Licencia-GPL--3.0-blue)
![Build](https://img.shields.io/badge/build-est%C3%A1tico-25MB-brightgreen)

*Hecho con cariño, para que instalarlo sea lo más fácil posible. 💙*

</div>

---

> ⚠️ **Aviso importante (léelo, por favor)**  
> Esto es un **self-bot**: se conecta con **tu cuenta personal de WhatsApp**, no con la API de negocios.  
> Puede **violar los Términos del Servicio de WhatsApp**. Úsalo bajo tu propio riesgo y en tu propia cuenta.

---

## 🚀 Inicio rápido en 60 segundos

TL;DR: **clonar → compilar → ejecutar → escanear el QR**.

```bash
git clone https://github.com/k4wai1/WhatsBotOnMySelf.git
cd WhatsBotOnMySelf
CGO_ENABLED=0 go build -ldflags="-s -w" -o whatsbot ./cmd/bot
./whatsbot
```

En la terminal aparecerá un QR. Escánéalo desde **WhatsApp → Dispositivos vinculados**.

¿No puedes escanear? Usa el código de vinculación (8 caracteres):

```bash
./whatsbot --pair 584144856737
```

> 💡 Sustituye el número por el de **tu** cuenta, con código de país y sin `+`.

Cuando todo salga bien verás:

```text
✅ Enlace establecido. Motor en línea.
```

👉 Sigue la guía completa en [docs/INSTALL.md](docs/INSTALL.md).

---

## 🧠 ¿Qué es y qué hace?

**WhatsBotOnMySelf** es un asistente que vive en tu WhatsApp. Le escribes un comando y él hace el trabajo: convierte stickers, transcribe audios, resume conversaciones, busca videos, genera imágenes con IA y mucho más.

Es **silencioso**: en vez de llenar el chat de mensajes, reacciona con un emoji sobre tu mensaje para decirte «estoy trabajando ⏳» o «ya está ✅». El texto solo aparece cuando hay un resultado útil que leer.

### ✨ Características

- 🧩 **Modular**: cada función es un addon independiente en `internal/addons/`.
- 🤫 **Interfaz limpia**: reacciones en vez de spam.
- 🔁 **Reconexión automática** con backoff si cae la conexión.
- 📦 **Un solo binario**: ~25 MB, estático, sin Node.js ni `node_modules`.
- 🔐 **Sesión propia** en `session.db` (SQLite), separada por cada instancia.
- 🛠️ **Herramientas reales**: usa `ffmpeg`, `ffprobe`, `yt-dlp`, `edge-tts` y Chrome cuando hace falta.
- 📚 **Ayuda viva**: `!help` se regenera solo cuando cambian los addons (usa DeepSeek).

---

## 🗂️ Comandos por categoría

Los comandos se escriben con un prefijo. Los prefijos por defecto son **`,` `!` `/`** (en la instancia *rupa* se usa **`.`**). En esta doc verás ejemplos con `!`.

| Categoría | Qué incluye | Enlace |
|---|---|---|
| 🎨 Stickers e imágenes | `.st`, `.sa`, `.ft`, `.fx`, `.cita`, `.wallpaper`, `.qr`, `.ss` | [Ver catálogo](docs/COMMANDS.md#-stickers-e-imágenes) |
| 🎵 Audio y voz | `.au`, `.tr`, `.resu`, `.voz`, `.cordial`, `.felix`, `.onreaccion` | [Ver catálogo](docs/COMMANDS.md#-audio-y-voz) |
| 🧠 IA y traducción | `.chat`, `.img`, `.unread`, `.definir`, `.bal`, `.help` | [Ver catálogo](docs/COMMANDS.md#-ia-y-traducción) |
| ⬇️ Descargas | `.ytdl`, `.fetch`, `.paste`, `.acortar`, `.unzip` | [Ver catálogo](docs/COMMANDS.md#-descargas-y-archivos) |
| 🛡️ Moderación y grupos | `.ban`, `.mute`, `.rm`, `.add`, `.pin`, `.top`, `.list` | [Ver catálogo](docs/COMMANDS.md#-moderación-y-grupos) |
| 🧰 Utilidades | `.ping`, `.rem`, `.a`, `.dado`, `.qr`, `.prefix`, `.files` | [Ver catálogo](docs/COMMANDS.md#-utilidades) |
| 💰 Información | `.tasas`, `.graf`, `.cedula`, `.git`, `.sysinfo`, `.info` | [Ver catálogo](docs/COMMANDS.md#-información) |
| 🎮 Diversión | `.gato`, `.blue`, `.cat`, `.furry`, `.sushi`, `.ttt`, `.love` | [Ver catálogo](docs/COMMANDS.md#-diversión) |

> 🔎 ¿No sabes cómo se llama? Escribe `!help` para ver el menú o `!help st` para ver los detalles de un comando.

---

## 🌳 Estructura del proyecto

```text
WhatsBotOnMySelf/
├── cmd/bot/main.go      ← punto de entrada: conexión, QR, reconexión
├── internal/
│   ├── addons/          ← los comandos (un archivo .go por función)
│   ├── core/            ← router, contexto y rutas
│   ├── config/          ← prefijos (assets/config.json)
│   └── store/           ← caché de contactos
├── assets/              ← fuentes, audios, imágenes y config.json
├── files/               ← archivos que el bot explorar con .files
├── statistics/          ← estadísticas de grupos
├── cache/               ← temporales
├── .env                 ← tus API keys (¡no compartir!)
├── .env.example         ← plantilla de configuración
├── session.db           ← tu sesión de WhatsApp (¡no compartir!)
└── whatsbot             ← el binario ya compilado
```

---

## 🔄 Actualizar

```bash
cd /home/kirara/WhatsBotOnMySelf   # o tu ruta
pm2 stop wsboms
git pull
CGO_ENABLED=0 go build -ldflags="-s -w" -o whatsbot ./cmd/bot
pm2 restart wsboms
pm2 logs wsboms --lines 30
```

> ⚠️ **Nunca reemplaces el binario mientras el bot está corriendo.** Detén primero, siempre.

Más detalles: [docs/INSTALL.md — Actualización segura](docs/INSTALL.md#-actualización-segura).

---

## 🛑 Detener

| Situación | Comando |
|---|---|
| Estás en la terminal | `Ctrl + C` |
| Está en PM2 | `pm2 stop wsboms` |
| Desde el propio WhatsApp | `!restart` (reinicia el proceso) |

---

## 📖 Documentación

- [Índice de documentación](docs/README.md)
- [Instalación paso a paso](docs/INSTALL.md)
- [Catálogo de comandos](docs/COMMANDS.md)
- [Preguntas frecuentes y solución de problemas](docs/FAQ.md)
- [Migrar desde la versión Node.js](docs/MIGRATION.md)

---

## 📄 Licencia

Copyright (C) 2026 kwai1 — **GPL v3**.  
Este programa es software libre: puedes redistribuirlo y modificarlo bajo los términos de la GNU General Public License v3 o posterior. Se distribuye **sin ninguna garantía**. Detalles: <https://www.gnu.org/licenses/gpl-3.0>.

Gracias a [whatsmeow](https://github.com/whatsmeow/whatsmeow), FFmpeg, yt-dlp, Deno, edge-tts y a todas las librerías que hacen posible este proyecto. 💙

**Repo:** <https://github.com/k4wai1/WhatsBotOnMySelf>
