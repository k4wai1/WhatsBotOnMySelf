// addons/tasas.js
module.exports = {
  commands: ['dolar', 'euro', 'yuan', 'lira', 'rublo', 'tasas'],
  handler: async (sock, msg, args) => {
    const jid = msg.key.remoteJid;

    // 1. Identificar qué comando específico detonó el enrutador
    // (Limpiamos cualquier prefijo para saber si pidió 'dolar', 'euro', etc.)
    const rawText = msg.message?.conversation || msg.message?.extendedTextMessage?.text || '';
    const commandUsed = rawText.trim().split(/\s+/)[0].toLowerCase().replace(/^[.!/,]/, '');

    // 2. Extraer el multiplicador
    // El router (index.js) ya extrajo el comando del array, así que el número está en args[0]
    let multiplicador = 1;
    if (args.length > 0) {
      // Reemplazamos coma por punto para permitir decimales (ej. 20,5)
      const num = parseFloat(args[0].replace(',', '.'));
      if (!isNaN(num) && num > 0) {
        // Límite de seguridad de 999 millones para evitar desbordamientos
        multiplicador = Math.min(num, 999999999);
      }
    }

    try {
      await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

      // Desactivamos rechazo de TLS por certificados conflictivos en la web del BCV
      process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0';

      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), 10000);
      const res = await fetch('https://www.bcv.org.ve/', { 
        headers: { 'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)' }, 
        signal: controller.signal 
      });
      clearTimeout(timer);

      if (!res.ok) throw new Error(`HTTP BCV Falló: ${res.status}`);
      const html = await res.text();

      const monedas = {
        'dolar': { codigo: 'USD', nombre: 'Dólar' },
        'euro': { codigo: 'EUR', nombre: 'Euro' },
        'yuan': { codigo: 'CNY', nombre: 'Yuan' },
        'lira': { codigo: 'TRY', nombre: 'Lira' },
        'rublo': { codigo: 'RUB', nombre: 'Rublo' }
      };

      const valores = {};
      for (const [id, info] of Object.entries(monedas)) {
        // Extracción mediante Regex directa al DOM del BCV
        const regex = new RegExp(`id=["']${id}["'][\\s\\S]*?<strong[^>]*>\\s*([\\d.,]+)\\s*<\\/strong>`, 'i');
        const match = html.match(regex);
        if (match && match[1]) {
          valores[id] = { valor: match[1].replace(/\s/g, ''), codigo: info.codigo, nombre: info.nombre };
        }
      }

      // =======================================================
      // COMANDO GENERAL: tasas
      // =======================================================
      if (commandUsed === 'tasas') {
        if (Object.keys(valores).length === 0) throw new Error('Estructura del BCV alterada temporalmente.');
        
        let respuesta = '*🏛️ Tasas de Cambio Oficiales (BCV)*\n\n';
        if (multiplicador !== 1) respuesta += `🧮 *Equivalencia para:* ${multiplicador}\n\n`;

        for (const [id, data] of Object.entries(valores)) {
          const precioNum = parseFloat(data.valor.replace(',', '.'));
          const total = precioNum * multiplicador;
          respuesta += `• ${data.nombre}: ${total.toFixed(2)} VES\n`;
        }
        
        const fechaMatch = html.match(/Fecha Valor:[^>]*>([^<]+)</);
        if (fechaMatch && fechaMatch[1]) respuesta += `\n📅 ${fechaMatch[1].trim()}`;
        
        await sock.sendMessage(jid, { text: respuesta }, { quoted: msg });
        await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
        return;
      }

      // =======================================================
      // COMANDOS ESPECÍFICOS: dolar, euro, etc.
      // =======================================================
      for (const [id, info] of Object.entries(monedas)) {
        if (commandUsed === id) {
          if (!valores[id]) throw new Error(`El valor de ${info.codigo} no está disponible en la página.`);

          const precioBCV = parseFloat(valores[id].valor.replace(',', '.'));
          const totalBCV = precioBCV * multiplicador;

          if (id === 'dolar') {
            let respuestaDolar = '';
            if (multiplicador !== 1) respuestaDolar += `🧮 *Calculando:* ${multiplicador} USD\n\n`;
            
            respuestaDolar += `🏛 *BCV:* ${totalBCV.toFixed(2)} VES\n`;

            // 🔸 EXTRACCIÓN BINANCE P2P
            try {
              const binanceRes = await fetch("https://p2p.binance.com/bapi/c2c/v2/friendly/c2c/adv/search", {
                method: "POST",
                headers: { "Content-Type": "application/json", "User-Agent": "Mozilla/5.0" },
                body: JSON.stringify({ "fiat": "VES", "page": 1, "rows": 1, "tradeType": "BUY", "asset": "USDT", "payTypes": [], "publisherType": "merchant" })
              });
              const binanceData = await binanceRes.json();
              
              if (binanceData.data && binanceData.data.length > 0) {
                const precioBinance = parseFloat(binanceData.data[0].adv.price);
                const totalBinance = precioBinance * multiplicador;
                const porcentaje = (((precioBinance - precioBCV) / precioBCV) * 100).toFixed(2);
                respuestaDolar += `🔸 *Binance:* ${totalBinance.toFixed(2)} VES\n📊 *Brecha:* ${porcentaje}%\n`;
              } else {
                respuestaDolar += `🔸 *Binance:* No disponible\n`;
              }
            } catch (e) {
              respuestaDolar += `🔸 *Binance:* Error de conexión\n`;
            }

            // 🔹 EXTRACCIÓN AIRTM
            try {
              const airtmRes = await fetch("https://rates.airtm.io/");
              const airtmData = await airtmRes.json();
              
              const vesUsdRate = airtmData.data && airtmData.data['ves/usd'];
              
              if (vesUsdRate && vesUsdRate.addValue) {
                const precioAirtm = parseFloat(vesUsdRate.addValue);
                const totalAirtm = precioAirtm * multiplicador;
                respuestaDolar += `🔹 *Airtm:* ${totalAirtm.toFixed(2)} VES\n`;
              } else {
                respuestaDolar += `🔹 *Airtm:* No disponible\n`;
              }
            } catch (e) {
              respuestaDolar += `🔹 *Airtm:* Error de red\n`;
            }

            await sock.sendMessage(jid, { text: respuestaDolar.trim() }, { quoted: msg });

          } else {
            // Lógica para monedas secundarias (Euro, Lira, Yuan, Rublo)
            let respuestaExtra = '';
            if (multiplicador !== 1) respuestaExtra += `🧮 *Calculando:* ${multiplicador} ${info.codigo}\n\n`;
            respuestaExtra += `🏛 *BCV:* ${totalBCV.toFixed(2)} VES`;
            await sock.sendMessage(jid, { text: respuestaExtra }, { quoted: msg });
          }
          
          await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
          return;
        }
      }

    } catch (err) {
      console.error('Error procesando el addon de tasas:', err.message);
      try { 
        await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } }); 
      } catch (e) { /* Fallback silencioso */ }
    } finally {
      process.env.NODE_TLS_REJECT_UNAUTHORIZED = '1'; 
    }
  }
};
