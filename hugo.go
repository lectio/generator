package hugo

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/gosimple/slug"
	"github.com/lectio/content"
	"github.com/lectio/harvester"
	"github.com/lectio/score"
)

// Generator is the primary Hugo content generator engine
type Generator struct {
	collection                         content.Collection
	destinationPath                    string
	simulateSocialScores               bool
	itemsInCollectionCount             int
	itemsGeneratedCount                int
	itemsWithFacebookGraphInvalidCount int
	itemsWithLinkedInGraphInvalidCount int
}

// GeneratedContent is a single Hugo page/content
type GeneratedContent struct {
	Link             string                         `json:"link"`
	Title            string                         `json:"title"`
	Summary          string                         `json:"description"`
	Body             string                         `json:"content"`
	Categories       []string                       `json:"categories"`
	CreatedOn        time.Time                      `json:"date"`
	FeaturedImage    string                         `json:"featuredimage"`
	Source           string                         `json:"source"`
	Slug             string                         `json:"slug"`
	TotalSharesCount int                            `json:"totalSharesCount"`
	FacebookGraph    *score.FacebookGraphResult     `json:"fbgraph"`
	LinkedInGraph    *score.LinkedInCountServResult `json:"ligraph"`
}

// NewGenerator creates the default Hugo generation engine
func NewGenerator(collection content.Collection, destinationPath string, simulateSocialScores bool) *Generator {
	result := new(Generator)
	result.collection = collection
	result.destinationPath = destinationPath
	result.simulateSocialScores = simulateSocialScores
	return result
}

// GetContentFilename returns the name of the file a given piece of GeneratedContent
func (g Generator) GetContentFilename(gc *GeneratedContent) string {
	return fmt.Sprintf("%s/%s.md", g.destinationPath, gc.Slug)
}

// GetActivitySummary returns a useful message about the activities that the engine performed
func (g Generator) GetActivitySummary() string {
	return fmt.Sprintf("Generated %d posts from %d items read (%q), Simulating scores: %v, Facebook Graph errors: %d, LinkedIn Graph errors: %d",
		g.itemsGeneratedCount, g.itemsInCollectionCount, g.collection.Source(),
		g.simulateSocialScores, g.itemsWithFacebookGraphInvalidCount, g.itemsWithLinkedInGraphInvalidCount)
}

// GenerateContent executes the engine (creates all the Hugo files from the given collection)
func (g *Generator) GenerateContent() error {
	items := g.collection.Content()
	g.itemsInCollectionCount = len(items)
	for i := 0; i < len(items); i++ {
		source := items[i]
		var genContent GeneratedContent
		var scores score.LinkScores

		genContent.Title = source.Title().Clean()
		genContent.Summary = source.Summary()
		genContent.Body = source.Body()
		genContent.Categories = source.Categories()
		genContent.CreatedOn = source.CreatedOn()
		if source.FeaturedImage() != nil {
			genContent.FeaturedImage = source.FeaturedImage().String()
		}

		switch v := source.(type) {
		case content.CuratedLink:
			url := v.Target()
			scores = score.GetLinkScores(&url, score.DefaultInitialTotalSharesCount, g.simulateSocialScores)
			genContent.Link = url.String()
			genContent.Source = harvester.GetSimplifiedHostname(&url)
			genContent.Slug = slug.Make(genContent.Source + "-" + source.Title().Clean())
			genContent.TotalSharesCount = scores.TotalSharesCount()
			genContent.FacebookGraph = scores.FacebookGraph()
			genContent.LinkedInGraph = scores.LinkedInCount()
		case content.Content:
			genContent.Slug = source.Keys().Slug()
		default:
			fmt.Printf("I don't know about type %T!\n", v)
		}

		if !genContent.FacebookGraph.IsValid() {
			g.itemsWithFacebookGraphInvalidCount++
		}
		if !genContent.LinkedInGraph.IsValid() {
			g.itemsWithLinkedInGraphInvalidCount++
		}

		err := genContent.createFile(g)
		if err != nil {
			return err
		}
		g.itemsGeneratedCount++
	}

	return nil
}

func (gc *GeneratedContent) createFile(g *Generator) error {
	fileName := g.GetContentFilename(gc)
	file, createErr := os.Create(fileName)
	if createErr != nil {
		return fmt.Errorf("Unable to create file %q: %v", fileName, createErr)
	}
	defer file.Close()

	frontMatter, fmErr := json.MarshalIndent(gc, "", "	")
	if fmErr != nil {
		return fmt.Errorf("Unable to marshal front matter %q: %v", fileName, fmErr)
	}

	_, writeErr := file.Write(frontMatter)
	if writeErr != nil {
		return fmt.Errorf("Unable to write front matter %q: %v", fileName, writeErr)
	}

	_, writeErr = file.WriteString("\n\n" + gc.Body)
	if writeErr != nil {
		return fmt.Errorf("Unable to write content body %q: %v", fileName, writeErr)
	}

	return nil
}
