import { useEffect, useRef, useState, useCallback } from 'react'
import type { GatewayEvent } from '../types'

export function useSSE(url: string, maxEvents = 100) {
  const [events, setEvents] = useState<GatewayEvent[]>([])
  const [connected, setConnected] = useState(false)
  const esRef = useRef<EventSource | null>(null)

  const clear = useCallback(() => setEvents([]), [])

  useEffect(() => {
    const es = new EventSource(url)
    esRef.current = es

    es.addEventListener('connected', () => {
      setConnected(true)
    })

    const eventTypes = [
      'challenge_issued',
      'payment_accepted',
      'payment_rejected',
      'http_request',
    ]

    for (const type of eventTypes) {
      es.addEventListener(type, (e) => {
        try {
          const event: GatewayEvent = JSON.parse(e.data)
          setEvents((prev) => {
            const next = [event, ...prev]
            return next.slice(0, maxEvents)
          })
        } catch {
          // ignore parse errors
        }
      })
    }

    es.onerror = () => {
      setConnected(false)
    }

    return () => {
      es.close()
      esRef.current = null
    }
  }, [url, maxEvents])

  return { events, connected, clear }
}
