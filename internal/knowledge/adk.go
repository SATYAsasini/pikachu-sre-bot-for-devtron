package knowledge

import (
	"context"
	"fmt"
	"io/fs"

	"google.golang.org/adk/tool/skilltoolset"
	"google.golang.org/adk/tool/skilltoolset/skill"
)

// packRoots are the directories ADK treats as skill roots. Its filesystem
// source does not recurse, so each class is mounted separately and merged.
var packRoots = []string{"packs/devtron", "packs/apps"}

// SkillSource exposes the embedded packs as an ADK skill source.
func SkillSource() (skill.Source, error) {
	var srcs []skill.Source
	for _, root := range packRoots {
		sub, err := fs.Sub(packFS, root)
		if err != nil {
			return nil, fmt.Errorf("mount %s: %w", root, err)
		}
		srcs = append(srcs, skill.NewFileSystemSource(sub))
	}
	return skill.NewMergedSource(srcs...), nil
}

// skillInstruction replaces ADK's generic wording. The default tells a model
// to load a skill when it "seems relevant"; for an SRE the useful rule is
// narrower and worth stating, because loading the wrong product's runbook is
// worse than loading none.
const skillInstruction = `You have a library of component knowledge. Each entry covers one thing that can
break: a Devtron platform microservice, or a well-known product such as Redis, Postgres or Kafka.
An entry states what the component does, how it actually fails in practice, the Prometheus metrics
it exposes and what each one means, and how a senior SRE remediates it.

1. Load an entry with ` + "`load_skill`" + ` only when you have concrete evidence that the component under
   investigation IS that thing: a Helm chart name, an image, an app.kubernetes.io label, or a prior
   identification. A similar-looking pod name is not evidence. Loading the wrong product's runbook
   produces confident, wrong advice.
2. Once loaded, prefer its named metrics over PromQL you invent. Those metric names were read out of
   the component's source, so they are the ones that actually exist.
3. ` + "`load_skill_resource`" + ` reads component.yaml inside an entry, which holds the same metric list in
   machine-readable form.
4. If nothing in the library matches, say so plainly and reason from Kubernetes evidence instead.
   Never invent product-specific guidance for a component you could not identify.`

// SkillToolset builds the ADK toolset that gives an agent progressive access
// to the packs: every request carries only the entry names and one-line
// descriptions, and the full document is fetched on demand. That keeps twenty
// components' worth of knowledge available at the cost of a short index.
func SkillToolset(ctx context.Context) (*skilltoolset.SkillToolset, error) {
	src, err := SkillSource()
	if err != nil {
		return nil, err
	}
	// Preload the frontmatters once: they are embedded, so the index is
	// built at startup instead of on every request.
	preloaded, _, err := skill.WithFrontmatterPreloadSource(ctx, src)
	if err != nil {
		return nil, fmt.Errorf("preload skill frontmatters: %w", err)
	}
	ts, err := skilltoolset.New(ctx, skilltoolset.Config{
		Source:            preloaded,
		Name:              "component_knowledge",
		SystemInstruction: skillInstruction,
	})
	if err != nil {
		return nil, fmt.Errorf("skill toolset: %w", err)
	}
	return ts, nil
}
