import type { Metadata } from 'next'
import { ClerkProvider } from '@clerk/nextjs'
import { Chakra_Petch, IBM_Plex_Sans, IBM_Plex_Mono } from 'next/font/google'
import './globals.css'
import { Providers } from './providers'
import { Nav } from '@/components/nav'
import { HexField } from '@/components/hex-field'

const display = Chakra_Petch({ subsets: ['latin'], weight: ['500', '600', '700'], variable: '--font-display' })
const sans = IBM_Plex_Sans({ subsets: ['latin'], weight: ['400', '500', '600'], variable: '--font-sans' })
const mono = IBM_Plex_Mono({ subsets: ['latin'], weight: ['400', '500'], variable: '--font-mono' })

export const metadata: Metadata = {
  title: 'inferoute · dispatch',
  description: 'Live view of the inferoute inference gateway: backends, traffic, cache, routing.',
}

// Dispatch palette, so Clerk's modals/pages match the app.
const clerkAppearance = {
  variables: {
    colorBackground: '#141a18',
    colorPrimary: '#c6f24e',
    colorText: '#e8edea',
    colorTextSecondary: '#8a9691',
    colorInputBackground: '#1c2320',
    colorInputText: '#e8edea',
    borderRadius: '0.375rem',
  },
  elements: {
    formButtonPrimary: 'text-void font-semibold',
    socialButtonsBlockButton: 'text-ink',
    socialButtonsBlockButtonText: 'text-ink',
    card: 'border border-line',
  },
}

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className={`${display.variable} ${sans.variable} ${mono.variable}`}>
      <body className="min-h-screen">
        <ClerkProvider appearance={clerkAppearance}>
          <HexField />
          <Providers>
            <Nav />
            <main className="relative z-10 mx-auto max-w-6xl px-4 pb-24 pt-6">{children}</main>
          </Providers>
        </ClerkProvider>
      </body>
    </html>
  )
}
