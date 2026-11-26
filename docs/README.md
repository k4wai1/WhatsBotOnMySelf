# 📚 Documentación de WhatsBotOnMySelf

¡Hola! 👋 Esta carpeta es tu manual de uso. Está pensada para **instalar y usar el bot sin dolor de cabeza**.

> 💡 **TL;DR:** clona → compila con Go → ejecuta → escanea el QR → escribe `!help` en WhatsApp.

---

## 🧭 ¿Por dónde empiezo?

| Situación | Documento |
|---|---|
| Quiero instalar el bot desde cero | [INSTALL.md — Instalación](INSTALL.md) |
| Quiero saber qué comandos existen | [COMMANDS.md — Comandos](COMMANDS.md) |
| Algo no funciona / tengo un error | [FAQ.md — Ayuda](FAQ.md) |
| Vengo de la versión antigua en Node.js | [MIGRATION.md — Migración](MIGRATION.md) |

---

## 🗺️ Mapa rápido

```text
docs/
├── README.md       ← estás aquí (índice)
├── INSTALL.md      ← requisitos, compilación, QR, PM2, systemd, seguridad
├── COMMANDS.md     ← catálogo completo, alias, flags y emojis de reacción
├── FAQ.md          ← errores típicos y cómo pedir ayuda
└── MIGRATION.md    ← Node.js → Go, sesión y cambios de comandos
```

---

## 🧩 Conceptos que conviene conocer

- **Self-bot**: el bot usa **tu cuenta personal**. Es bajo tu propio riesgo. ⚠️
- **Binario**: `whatsbot` se compila en la raíz con Go. No hay `index.js` ni `node_modules`.
- **Prefijo**: el carácter que va antes del comando. Por defecto `,`, `!` y `/`. Ejemplo: `!st`.
- **`session.db`**: la sesión de WhatsApp. **Un directorio = una cuenta = una sesión.**
- **Reacciones**: los estados (`⏳`, `✅`, `❌`) van como emoji sobre tu mensaje, no como texto.

---

## 🔗 Enlaces útiles

- [README principal](../README.md)
- [Repositorio en GitHub](https://github.com/k4wai1/WhatsBotOnMySelf)
- [Plantilla de configuración](../.env.example)

---

*¿Falta algo en esta guía? Ábrelo en los Issues del repo con el mayor contexto posible (logs incluidos, sin secretos). 💙*
