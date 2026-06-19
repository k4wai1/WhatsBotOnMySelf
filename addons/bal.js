// addons/bal.js

module.exports = {
  commands: ['bal'],
  handler: async (sock, msg, args) => {
    const jid = msg.key.remoteJid;
      
    try {
      await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

      const response = await fetch('https://api.deepseek.com/user/balance', {
        // Obtenemos la KEY directamente del archivo .env
        headers: { 'Authorization': `Bearer ${process.env.DEEPSEEK_API_KEY}` }
      });

      if (!response.ok) throw new Error(`Error en API HTTP: ${response.status}`);
      
      const data = await response.json();
      const balance = data.balance_infos?.[0]?.total_balance || '0.00';
      const currency = data.balance_infos?.[0]?.currency || 'USD';

      await sock.sendMessage(jid, { 
          text: `💰 *Balance DeepSeek*\n\n💵 Saldo: *${balance} ${currency}*`
      }, { quoted: msg });
      
      await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

    } catch (error) {
      console.error('Error en addon .bal:', error.message);
      await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
    }
  }
};
