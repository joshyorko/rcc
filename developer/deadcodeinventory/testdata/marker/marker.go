package marker

type Marker interface {
	Mark()
}

type Token struct{}

func (Token) Mark() {}
