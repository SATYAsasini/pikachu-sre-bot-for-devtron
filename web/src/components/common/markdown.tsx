import { memo } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { cn } from 'cn'

/**
 * Markdown renderer for the analysis and the SRE report.
 *
 * react-markdown with remark-gfm, no rehype-raw: the content comes from a model
 * and must never be able to inject HTML into this page. Styling lives in the
 * `.md` rules in index.css so both themes are handled in one place.
 */
export const Markdown = memo(function Markdown({
  children,
  tight,
  className,
}: {
  children: string
  /** Smaller line height for markdown nested inside cards. */
  tight?: boolean
  className?: string
}) {
  return (
    <div className={cn('md', tight && 'md-tight', className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          a: ({ href, children: kids }) => (
            <a href={href} target="_blank" rel="noreferrer noopener">
              {kids}
            </a>
          ),
        }}
      >
        {children}
      </ReactMarkdown>
    </div>
  )
})
