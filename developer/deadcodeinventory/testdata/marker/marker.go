package marker

type Marker interface {
	Mark()
}

type Token struct{}

func (Token) Mark() {}

type LocalToken struct{}

func (LocalToken) LocalMark() {}

func useAnonymousInterface() {
	var _ interface{ LocalMark() } = LocalToken{}
}

type NamedToken struct{}

func (NamedToken) NamedMark() {}

func useLocalInterface() {
	type local interface{ NamedMark() }
	var _ local = NamedToken{}
}
