// addons/chart_generator.js
// Helper interno: genera la gráfica de tasas (BCV vs Binance) con QuickChart.
// Usado por channel.js. No registra comandos.
const fs = require('fs');
const path = require('path');

// --- CONFIGURACIÓN ---
const STATS_DIR = path.join(__dirname, '..', 'assets', 'statistics');
const HISTORY_FILE = path.join(STATS_DIR, 'rates_history.json');
const CHART_OUTPUT = path.join(STATS_DIR, 'rates_chart.png');

function getHistoryData() {
    try {
        if (!fs.existsSync(HISTORY_FILE)) return null;

        const history = JSON.parse(fs.readFileSync(HISTORY_FILE, 'utf8'));
        let allData = [...(history.daily || []), ...(history.intraday || [])];

        // Quedarse con el último registro de cada día para la gráfica
        const uniqueData = {};
        allData.forEach(item => {
            if (item.bcv_usd && item.binance_usdt) {
                uniqueData[item.date] = item;
            }
        });

        // Ordenar por fecha y tomar solo los últimos 30 días
        const sorted = Object.values(uniqueData).sort((a, b) => a.date.localeCompare(b.date));
        return sorted.slice(-30);

    } catch (e) {
        console.error('[CHART] Error leyendo historial:', e.message);
        return null;
    }
}

async function generateChart() {
    console.log('[CHART] 🎨 Generando gráfica ligera con QuickChart...');

    const data = getHistoryData();
    if (!data || data.length === 0) {
        console.log('[CHART] ⚠️ No hay datos para graficar');
        return null;
    }

    // Preparar datos para Chart.js
    const labels = data.map(d => {
        const parts = d.date.split('-');
        return parts.length === 3 ? `${parts[2]}/${parts[1]}` : d.date;
    });
    const bcvData = data.map(d => parseFloat(d.bcv_usd));
    const binanceData = data.map(d => parseFloat(d.binance_usdt));

    const chartConfig = {
        type: 'line',
        data: {
            labels: labels,
            datasets: [
                {
                    label: 'BCV Oficial',
                    data: bcvData,
                    borderColor: '#2962FF',
                    backgroundColor: 'rgba(41, 98, 255, 0.1)',
                    borderWidth: 2,
                    fill: true,
                    tension: 0.4,
                    pointRadius: 0,
                    pointHitRadius: 10
                },
                {
                    label: 'Binance P2P',
                    data: binanceData,
                    borderColor: '#F7931A',
                    backgroundColor: 'rgba(247, 147, 26, 0.1)',
                    borderWidth: 2,
                    fill: true,
                    tension: 0.4,
                    pointRadius: 0,
                    pointHitRadius: 10
                }
            ]
        },
        options: {
            title: {
                display: true,
                text: 'Dólar en Venezuela - Últimos 30 días',
                fontColor: '#D1D4DC',
                fontSize: 18,
                padding: 20
            },
            legend: {
                labels: { fontColor: '#D1D4DC', usePointStyle: true, padding: 20 },
                position: 'bottom'
            },
            scales: {
                xAxes: [{ gridLines: { color: '#2B2F3A', drawBorder: false }, ticks: { fontColor: '#D1D4DC', maxTicksLimit: 10 } }],
                yAxes: [{ gridLines: { color: '#2B2F3A', drawBorder: false }, ticks: { fontColor: '#D1D4DC' } }]
            },
            layout: { padding: { left: 10, right: 20, top: 10, bottom: 10 } }
        }
    };

    try {
        const response = await fetch('https://quickchart.io/chart', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                width: 800,
                height: 450,
                devicePixelRatio: 2.0,
                format: 'png',
                backgroundColor: '#1E222D', // Fondo oscuro garantizado
                chart: chartConfig
            })
        });

        if (!response.ok) throw new Error(`Error HTTP: ${response.status}`);

        const arrayBuffer = await response.arrayBuffer();
        fs.writeFileSync(CHART_OUTPUT, Buffer.from(arrayBuffer));

        console.log(`[CHART] ✅ Gráfica guardada: ${CHART_OUTPUT}`);
        return CHART_OUTPUT;

    } catch (e) {
        console.error('[CHART] ❌ Error generando gráfica:', e.message);
        return null;
    }
}

function getChartPath() {
    return fs.existsSync(CHART_OUTPUT) ? CHART_OUTPUT : null;
}

module.exports = { generateChart, getChartPath };
