/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        ink: '#080c12',
        panel: '#101720',
        panel2: '#151f2b',
        line: '#263544',
        cyan: '#5ee7d0',
        sky: '#75b8ff',
        amber: '#f3ba67',
        danger: '#f47f80',
      },
      boxShadow: {
        panel: '0 18px 60px rgba(0, 0, 0, 0.24)',
      },
      fontFamily: {
        sans: ['Inter', 'ui-sans-serif', 'system-ui', 'sans-serif'],
        mono: ['"SFMono-Regular"', 'Consolas', '"Liberation Mono"', 'monospace'],
      },
    },
  },
  plugins: [],
}
