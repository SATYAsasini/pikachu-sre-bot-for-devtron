import { useMemo, useState } from 'react'

/**
 * Paging for lists that would otherwise run off the bottom of the page.
 *
 * A hundred alerts rendered as a hundred rows is not a list any more, it is a
 * document — you lose your place, the scrollbar stops meaning anything, and
 * every surrounding control ends up a page-length away. Everything here is
 * filtered client-side from data already in memory, so paging costs nothing
 * and is purely about keeping a screen a screen.
 */
export function usePaged<T>(items: readonly T[], perPage: number) {
  const [wanted, setWanted] = useState(1)
  const pages = Math.max(1, Math.ceil(items.length / perPage))

  // Clamped during render rather than corrected in an effect. An effect would
  // paint one frame of an empty page before snapping back, which is exactly
  // what it looks like when a filter shrinks the list under you.
  const page = Math.min(wanted, pages)

  const slice = useMemo(() => items.slice((page - 1) * perPage, page * perPage), [items, page, perPage])

  return {
    page,
    pages,
    setPage: setWanted,
    slice,
    total: items.length,
    from: items.length === 0 ? 0 : (page - 1) * perPage + 1,
    to: Math.min(page * perPage, items.length),
  }
}
