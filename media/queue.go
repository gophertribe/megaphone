package media

import "errors"

var ErrSessionNotFound = errors.New("session not found")

type Session interface {
	GetID() string
}

type Queue struct {
	current Session
	wait    []Session
}

func (queue *Queue) GetSession(id string) (Session, error) {
	if queue.current != nil && queue.current.GetID() == id {
		return queue.current, nil
	}
	for i := range queue.wait {
		if queue.wait[i].GetID() == id {
			return queue.wait[i], nil
		}
	}
	return nil, ErrSessionNotFound
}

func (queue *Queue) Enqueue(session Session) error {
	queue.wait = append(queue.wait, session)
	// TODO: copy queuing logic
	return nil
}

func NewQueue() *Queue {
	return &Queue{}
}
