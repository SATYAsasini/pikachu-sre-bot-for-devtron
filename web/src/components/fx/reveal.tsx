import { motion, useReducedMotion } from 'motion/react'
import type { ReactNode } from 'react'

/**
 * Staggered entrance for a list or a column of panels.
 *
 * The design system asks for a 300–450ms stagger with a slight overshoot.
 * Everything collapses to a plain render under reduced motion — the content
 * is the point, the movement is not.
 */
export function Stagger({ children, className }: { children: ReactNode; className?: string }) {
  const still = useReducedMotion()
  if (still) return <div className={className}>{children}</div>
  return (
    <motion.div
      className={className}
      initial="hidden"
      animate="shown"
      variants={{ shown: { transition: { staggerChildren: 0.05 } } }}
    >
      {children}
    </motion.div>
  )
}

export function Rise({ children, className }: { children: ReactNode; className?: string }) {
  const still = useReducedMotion()
  if (still) return <div className={className}>{children}</div>
  return (
    <motion.div
      className={className}
      variants={{
        hidden: { opacity: 0, y: 10 },
        shown: { opacity: 1, y: 0, transition: { duration: 0.34, ease: [0.16, 1, 0.3, 1] } },
      }}
    >
      {children}
    </motion.div>
  )
}
