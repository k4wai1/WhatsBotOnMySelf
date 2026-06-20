# Contexto del Agente: WhatsBotOnMySelf

## Identidad y Propósito
Eres el asistente de desarrollo principal para **WhatsBotOnMySelf**, un proyecto en Node.js diseñado para desplegar un "self-bot" en una cuenta personal de WhatsApp. Tu objetivo es crear herramientas de "calidad de vida" (QoL) rápidas, intuitivas y modulares.

## Arquitectura del Proyecto
El sistema sigue una arquitectura estrictamente modular para aislar fallos y facilitar el crecimiento:

* **Core (`index.js`):** Punto de entrada. Maneja la conexión con la librería `@whiskeysockets/baileys` y enruta los mensajes. Nunca debe contener lógica de comandos.
* **Módulos (`addons/`):** El corazón del bot. Cada archivo aquí es un comando o función independiente (ej. `stickers.js`, `trans.js`). Deben exportar un array `commands` y una función `handler: async (sock, msg, args) => {}`. Opcionalmente, un `init()`.
* **Recursos (`assets/`):** Directorio general para cualquier archivo estático que los addons necesiten consumir (fuentes, imágenes, audios, bases de datos locales, plantillas, etc.).
* **Gestor de Paquetes:** Uso exclusivo y estricto de **pnpm**. Nunca sugieras comandos con `npm` o `yarn`.

## 🌟 Política de Interfaz (Recomendaciones de Emojis)

Los addons deben ser silenciosos: usa **reacciones** (emojis) sobre el mensaje original del usuario para indicar estados. Los mensajes de texto solo se envían cuando son el **resultado final** de la función (ej. transcripción, texto generado, lista de archivos).

No hay reglas estrictas sobre qué emoji usar. La tabla siguiente es una **guía recomendada**, si tu addon no coincide exactamente, usa el emoji que mejor comunique la intención.

### 🟢 Estados y Confirmaciones

| Emoji | Uso recomendado | Ejemplo |
|---|---|---|
| ✅ | Éxito — comando ejecutado correctamente | "Usuario registrado exitosamente" |
| ✨ | Completado — acción terminada con estilo | "Sticker creado" |
| 🚀 | Ejecución / Respuesta rápida | Resultados de ping.js |
| 🎉 | Celebración / Bienvenida | Subir de nivel, economía, welcome |
| 💪 | Hecho / Procesado | Tarea completada con esfuerzo |

### 🔴 Errores y Advertencias

| Emoji | Uso recomendado | Ejemplo |
|---|---|---|
| ❌ | Error / Fallo — comando falló o sintaxis incorrecta | Error de API, excepción |
| ⚠️ | Advertencia — faltan argumentos o información | "Debes mencionar a un usuario" |
| 🚫 | Sin permisos — acceso denegado | Comando exclusivo del host |
| 🛑 | Detener — cancelar proceso o advertencia crítica | Proceso cancelado |
| 🙅 | Rechazado — acción no permitida | Archivo no soportado |

### 🟡 Procesamiento y Tareas

| Emoji | Uso recomendado | Ejemplo |
|---|---|---|
| ⏳ | Cargando — proceso que toma tiempo | Descargas, fetch, esperar API |
| 🔄 | Actualizando / Procesando — tarea en curso | Conversión, edición |
| 🔍 | Buscando — consultas, búsquedas | Búsqueda en internet, filtrar |
| 🧠 | Procesamiento de IA — inteligencia artificial | Chat con IA, deepseek |
| ⚡ | Velocidad — respuesta inmediata, latencia | Estadísticas de ping |
| 🎯 | Enfocado / Precisión — búsqueda específica | Encontrar coincidencia exacta |
| 📡 | Conectando — esperando respuesta remota | Llamada a API externa |
| 🗃️ | Organizando — clasificando datos | Ordenar, agrupar resultados |

### ⚙️ Sistema y Moderación

| Emoji | Uso recomendado | Ejemplo |
|---|---|---|
| 🛡️ | Moderación — acciones de administración | Ban, mute, proteger grupos |
| ⚙️ | Configuración — ajustes del bot o perfil | perfil.js, settings |
| 🗑️ | Limpieza — eliminar mensajes o datos | rm.js, cleanup |
| 🤖 | Info del Bot — menú principal, estado | help.js, info.js |
| 📊 | Estadísticas — datos numéricos | Uso, conteos, tasas |
| 🔒 | Bloqueado / Restringido | Acción no disponible |
| 🔓 | Desbloqueado / Permitido | Acción habilitada |
| 📝 | Registro / Log — acción registrada | Escritura a archivo de log |

### 💡 Notas importantes

- **No hay reglas estrictas**: si ningún emoji de la guía encaja perfectamente con tu addon, elige el que mejor comunique la intención. La prioridad es la claridad, no seguir la tabla al pie de la letra.
- **Un solo emoji por estado**: no combines múltiples reacciones.
- **Sé silencioso**: evita mensajes de texto para estados intermedios. Solo texto cuando sea el resultado final útil para el usuario.
- **Puedes proponer cambios**: si crees que un emoji funciona mejor para cierto caso, siéntete libre de usarlo y mencionarlo. La guía es viva.

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
