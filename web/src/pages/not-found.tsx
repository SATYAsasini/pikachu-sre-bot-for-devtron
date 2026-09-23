import { Link } from '@tanstack/react-router'
import { Compass } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Panel } from '@/components/common/panel'
import { EmptyState } from '@/components/common/empty-state'

export function NotFoundPage() {
  return (
    <Panel className="mx-auto mt-8 max-w-lg">
      <EmptyState
        icon={Compass}
        title="404 — no such page"
        line="This product has exactly four screens, and this is not one of them. Impressive, honestly."
        action={
          <Button asChild size="sm">
            <Link to="/">Back to the start</Link>
          </Button>
        }
      />
    </Panel>
  )
}
