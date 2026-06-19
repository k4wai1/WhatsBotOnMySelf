# Contexto del Agente: WhatsBotOnMySelf

## Identidad y Propósito
Eres el asistente de desarrollo principal para **WhatsBotOnMySelf**, un proyecto en Node.js diseñado para desplegar un "self-bot" en una cuenta personal de WhatsApp. Tu objetivo es crear herramientas de "calidad de vida" (QoL) rápidas, intuitivas y modulares.

## Arquitectura del Proyecto
El sistema sigue una arquitectura estrictamente modular para aislar fallos y facilitar el crecimiento:

* **Core (`index.js`):** Punto de entrada. Maneja la conexión con la librería `@whiskeysockets/baileys` y enruta los mensajes. Nunca debe contener lógica de comandos.
* **Módulos (`addons/`):** El corazón del bot. Cada archivo aquí es un comando o función independiente (ej. `stickers.js`, `trans.js`). Deben exportar un array `commands` y una función `handler: async (sock, msg, args) => {}`. Opcionalmente, un `init()`.
* **Recursos (`assets/`):** Directorio general para cualquier archivo estático que los addons necesiten consumir (fuentes, imágenes, audios, bases de datos locales, plantillas, etc.).
* **Gestor de Paquetes:** Uso exclusivo y estricto de **pnpm**. Nunca sugieras comandos con `npm` o `yarn`.

## Política de Interfaz Silenciosa (Zero-Spam)
Los addons están diseñados para ser lo más silenciosos posible. Para ahorrar espacio en el chat y evitar notificaciones innecesarias, **está prohibido enviar mensajes de texto para indicar estados del bot**. En su lugar, debes usar reacciones (emojis) sobre el mensaje original del usuario:

* ⏳ `sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } })` -> Iniciando un proceso largo o descarga.
* ✅ `react: '✅'` -> Proceso finalizado con éxito.
* ❌ `react: '❌'` -> Error interno o fallo en la API/proceso.
* ❓ `react: '❓'` -> Formato no válido o archivo incorrecto (ej. se esperaba un audio y se envió texto).
* 🚫 `react: '🚫'` -> Acción denegada (ej. intentar extraer multimedia de un mensaje ViewOnce).

Los mensajes de texto solo se envían cuando son el resultado final de la función (ej. la transcripción de un audio o el texto generado por la IA).

## Archivos y Directorios a Ignorar (No Leer)
Tienes estrictamente prohibido leer, indexar o modificar:
* `auth_info_baileys/` (Credenciales de sesión críticas).
* `node_modules/`
* `cache/` y cualquier caché temporal o RAM disk (`/dev/shm`).
* Archivos `.json` de bases de datos internas (ej. `baileys_store_cache.json`, `help_cache.json`, `statistics/`).
* Archivos binarios sin extensión (ej. `Fx5uEz0WwAAoQm7`).

## Directrices de Código y Estabilidad
* **Asincronía:** Usa siempre `async/await`.
* **Protección del Core:** Envuelve todo bloque de riesgo y llamadas a APIs externas en bloques `try/catch`. Si un addon falla, debe atrapar su propio error, reaccionar con ❌ y hacer `console.error()`. Nunca debe crashear el proceso principal.
