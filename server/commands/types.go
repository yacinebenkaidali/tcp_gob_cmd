package commands

type Command struct {
	Action  string
	Payload any // Can be any type you register with gob
}
