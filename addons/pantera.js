// addons/pantera.js
// Mensajes glitch (Zalgo) o censurados aleatorios, puro entretenimiento.
// Portado del bot de rupa y adaptado al router de addons.
module.exports = {
    commands: ['pantera'],

    handler: async (sock, msg) => {
        const jid = msg.key.remoteJid;

        try {
            // Colección de frases random y de broma
            const bromas = [
                "Hackeando la base de datos central... Por favor espere.",
                "Descargando más memoria RAM en tu dispositivo...",
                "El servidor Debian del bot ha tomado conciencia propia. Huye.",
                "Iniciando secuencia de autodestrucción en 3... 2... 1...",
                "Tu historial de búsqueda ha sido comprometido.",
                "Formateando disco duro remoto... 99% completado.",
                "Detectando anomalía en la matrix... Reiniciando simulación.",
                "Despertando a los antiguos dioses del código fuente...",
                "si les esto eres gay",
                "dios nos abandono, por eso jesus se fue de este mundo",
                "linux lo mas duro del sistema",
                "estos mesajes no los devra ver nadie, que haces aqui?, no es un lugar para alguien de ese mundo"
            ];

            // Función matemática para generar texto "Zalgo" (Glitch)
            const generarZalgo = (texto) => {
                // Matriz de caracteres Unicode que se superponen (marcas diacríticas)
                const caracteresGlitch = [
                    '\u030d', '\u030e', '\u0304', '\u0305', '\u033f', '\u0311', '\u0306', '\u0310',
                    '\u0352', '\u0357', '\u0351', '\u0301', '\u0340', '\u0300', '\u0341', '\u030f',
                    '\u0312', '\u0313', '\u0314', '\u030c', '\u030a', '\u0328', '\u0316', '\u0317',
                    '\u0318', '\u0319', '\u031c', '\u031d', '\u0336', '\u0334', '\u0335', '\u0337', '\u0338'
                ];

                return texto.split('').map(char => {
                    if (char === ' ') return char; // No glitcheamos los espacios para que sea legible
                    let zalgoChar = char;
                    const nivelGlitch = Math.floor(Math.random() * 10) + 4; // Entre 4 y 13 marcas por letra
                    for (let i = 0; i < nivelGlitch; i++) {
                        zalgoChar += caracteresGlitch[Math.floor(Math.random() * caracteresGlitch.length)];
                    }
                    return zalgoChar;
                }).join('');
            };

            // Función para generar bloques censurados "████ ██"
            const generarCensurado = () => {
                const longitudes = [3, 4, 2, 5, 7, 3, 2, 6, 4]; // Tamaños de bloques aleatorios
                let resultado = [];
                const cantidadPalabras = Math.floor(Math.random() * 8) + 4; // De 4 a 11 bloques
                for (let i = 0; i < cantidadPalabras; i++) {
                    const len = longitudes[Math.floor(Math.random() * longitudes.length)];
                    resultado.push('█'.repeat(len));
                }
                return resultado.join(' ');
            };

            // Lógica "random" para decidir qué susto enviar
            const estilo = Math.floor(Math.random() * 3);
            let respuestaFinal = '';

            if (estilo === 0) {
                // 33% de probabilidad: solo bloques censurados puros
                respuestaFinal = generarCensurado() + " " + generarCensurado();
            } else if (estilo === 1) {
                // 33% de probabilidad: una broma totalmente glitcheada
                const fraseAleatoria = bromas[Math.floor(Math.random() * bromas.length)];
                respuestaFinal = generarZalgo(fraseAleatoria);
            } else {
                // 33% de probabilidad: mezcla de error glitcheado y censura
                const fraseAleatoria = bromas[Math.floor(Math.random() * bromas.length)];
                respuestaFinal = generarZalgo("ERROR 404: ") + generarCensurado() + "\n" + generarZalgo(fraseAleatoria);
            }

            await sock.sendMessage(jid, { text: respuestaFinal }, { quoted: msg });

        } catch (error) {
            // Hasta el error va glitcheado para mantener el estilo
            await sock.sendMessage(jid, { text: 'e̶r̶r̶o̶r̶ ̶c̶r̶i̶t̶i̶c̶o̶ ̶d̶e̶l̶ ̶s̶i̶s̶t̶e̶m̶a̶' }, { quoted: msg });
        }
    }
};
