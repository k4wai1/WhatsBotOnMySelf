// addons/github.js
const REPO_URL = 'https://github.com/k4wai1/WhatsBotOnMySelf';

module.exports = {
    commands: ['git', 'github', 'repo'],

    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;
        try {
            await sock.sendMessage(jid, { react: { text: '🚀', key: msg.key } });
            await sock.sendMessage(jid, {
                text: [
                    '🤖 *WhatsBotOnMySelf*',
                    '',
                    `🔗 ${REPO_URL}`
                ].join('\n')
            }, { quoted: msg });
        } catch (error) {
            console.error('Error en github.js:', error);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
        }
    }
};
