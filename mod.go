package gekko

type Module interface {
	Install(app *App, commands *Commands)
}
