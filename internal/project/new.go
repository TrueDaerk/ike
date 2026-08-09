package project

// new.go is the project-side of "New Project" (#1718): the message that opens
// the wizard. Which languages offer scaffolding and how a project is
// populated lives in internal/lang (ProjectScaffolder); the wizard dialog and
// the create/switch routing live in internal/app. Target (clone.go) decides
// where a new project may land — the same rules as a clone.

// OpenNewProjectMsg asks the root model to open the new-project wizard.
// Dispatched by the project.new command.
type OpenNewProjectMsg struct{}
