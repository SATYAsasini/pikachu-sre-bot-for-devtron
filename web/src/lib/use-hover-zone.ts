import { useEffect, useRef, useState } from 'react'

/**
 * Hover tracking for an element that changes size when hovered.
 *
 * `onMouseEnter`/`onMouseLeave` cannot express this. The hero collapses when
 * the pointer is over it, collapsing shrinks it, and the shrink pulls its own
 * edge out from under the pointer — which fires `mouseleave`, which expands it
 * back under the pointer, which fires `mouseenter`. It oscillates forever, and
 * a time delay only changes how fast it flickers.
 *
 * The fix is to stop the exit boundary from moving. The rectangle is measured
 * while the element is at rest and then frozen, so the region being tested is
 * the *expanded* one for as long as the pointer stays inside it. Leaving is
 * tested against that frozen box plus a margin, so the boundary the pointer
 * crosses is always the same boundary it entered through.
 *
 * Returns whether the pointer is in the zone. Touch never enters it: there is
 * no hover on a touchscreen, and a tap should not toggle a layout.
 */
export function useHoverZone<T extends HTMLElement>(margin = 24): [React.Ref<T>, boolean] {
  const ref = useRef<T>(null)
  const [inside, setInside] = useState(false)
  // The boundary, captured at rest and held for as long as we are inside it.
  const frozen = useRef<DOMRect | null>(null)
  const frame = useRef(0)

  useEffect(() => {
    const onMove = (e: PointerEvent) => {
      if (e.pointerType === 'touch') return
      // One test per frame. pointermove fires far faster than the layout can
      // possibly matter, and this runs on every pixel of every mouse move on
      // the page.
      if (frame.current) return
      frame.current = requestAnimationFrame(() => {
        frame.current = 0
        const el = ref.current
        if (!el) return

        const live = el.getBoundingClientRect()
        // While outside, the live box is the truth. While inside, the frozen
        // one is — that is the whole point.
        const box = frozen.current ?? live
        const hit =
          e.clientX >= box.left - margin &&
          e.clientX <= box.right + margin &&
          e.clientY >= box.top - margin &&
          e.clientY <= box.bottom + margin

        setInside((was) => {
          if (hit && !was) frozen.current = live
          if (!hit && was) frozen.current = null
          return hit
        })
      })
    }

    // Leaving the window counts as leaving the zone; otherwise the hero stays
    // collapsed forever after the pointer exits the top of the page.
    const onLeave = () => {
      frozen.current = null
      setInside(false)
    }

    window.addEventListener('pointermove', onMove, { passive: true })
    document.addEventListener('pointerleave', onLeave)
    return () => {
      window.removeEventListener('pointermove', onMove)
      document.removeEventListener('pointerleave', onLeave)
      if (frame.current) cancelAnimationFrame(frame.current)
    }
  }, [margin])

  return [ref, inside]
}
