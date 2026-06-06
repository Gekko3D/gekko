package app

import "testing"

func TestSetTextOverlayItemsCopiesItemsAndClearsVertexCount(t *testing.T) {
	app := &App{}
	items := []TextOverlayItem{
		{
			Text:     "overlay",
			Position: [2]float32{12, 24},
			Scale:    0.75,
			Color:    [4]float32{1, 1, 1, 1},
		},
	}

	app.SetTextOverlayItems(items)
	items[0].Text = "mutated"
	items[0].Position = [2]float32{99, 99}

	if got := app.TextResources.Items[0].Text; got != "overlay" {
		t.Fatalf("expected text item to be copied, got %q", got)
	}
	if got := app.TextResources.Items[0].Position; got != [2]float32{12, 24} {
		t.Fatalf("expected text position to be copied, got %+v", got)
	}

	app.TextResources.VertexCount = 12
	app.SetTextOverlayItems(items[:0])

	if got := len(app.TextResources.Items); got != 0 {
		t.Fatalf("expected no text items after empty set, got %d", got)
	}
	if app.TextResources.VertexCount != 0 {
		t.Fatalf("expected vertex count reset, got %d", app.TextResources.VertexCount)
	}

	app.SetTextOverlayItems([]TextOverlayItem{
		{
			Text:     "copied",
			Position: [2]float32{1, 2},
			Scale:    3,
			Color:    [4]float32{4, 5, 6, 7},
		},
	})
	if got := app.TextResources.Items[0].Text; got != "copied" {
		t.Fatalf("expected copied text item, got %q", got)
	}
	if got := app.TextResources.Items[0].Position; got != [2]float32{1, 2} {
		t.Fatalf("expected copied text position, got %+v", got)
	}
}

func TestAppendTextOverlayItemsPreservesExistingImmediateText(t *testing.T) {
	app := &App{}
	app.DrawText("ui", 1, 2, 0.5, [4]float32{1, 1, 1, 1})
	app.TextResources.VertexCount = 12

	items := []TextOverlayItem{
		{
			Text:     "ecs",
			Position: [2]float32{12, 24},
			Scale:    0.75,
			Color:    [4]float32{0.5, 0.5, 1, 1},
		},
	}
	app.AppendTextOverlayItems(items)
	items[0].Text = "mutated"

	if got := len(app.TextResources.Items); got != 2 {
		t.Fatalf("expected immediate and appended text items, got %d", got)
	}
	if got := app.TextResources.Items[0].Text; got != "ui" {
		t.Fatalf("expected existing immediate text to be preserved, got %q", got)
	}
	if got := app.TextResources.Items[1].Text; got != "ecs" {
		t.Fatalf("expected appended text item to be copied, got %q", got)
	}
	if app.TextResources.VertexCount != 0 {
		t.Fatalf("expected vertex count reset after append, got %d", app.TextResources.VertexCount)
	}
}
