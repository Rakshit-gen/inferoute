import type { Config } from 'tailwindcss'

const config: Config = {
  content: ['./app/**/*.{ts,tsx}', './components/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        void: 'var(--void)',
        panel: 'var(--panel)',
        'panel-2': 'var(--panel-2)',
        line: 'var(--line)',
        ink: 'var(--ink)',
        'ink-dim': 'var(--ink-dim)',
        route: 'var(--route)',
        cache: 'var(--cache)',
        alert: 'var(--alert)',
        warn: 'var(--warn)',
      },
      fontFamily: {
        display: ['var(--font-display)', 'ui-monospace', 'monospace'],
        sans: ['var(--font-sans)', 'system-ui', 'sans-serif'],
        mono: ['var(--font-mono)', 'ui-monospace', 'monospace'],
      },
      borderRadius: {
        DEFAULT: '0.375rem',
      },
      keyframes: {
        'led-pulse': {
          '0%, 100%': { opacity: '1' },
          '50%': { opacity: '0.35' },
        },
        pip: {
          '0%': { left: '0%', opacity: '0' },
          '8%': { opacity: '1' },
          '92%': { opacity: '1' },
          '100%': { left: '100%', opacity: '0' },
        },
      },
      animation: {
        'led-pulse': 'led-pulse 1.8s ease-in-out infinite',
        pip: 'pip 2.4s linear infinite',
      },
    },
  },
  plugins: [],
}

export default config
