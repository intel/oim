# We use the http://plantuml.com/plantuml server to generate
# images. That way nothing needs to be installed besides Go.
# The client tool has no license, so we can't vendor it.
# Instead we "go get" it if (and only if) needed.
DOC_PLANTUML_GO = $(GOPATH)/bin/plantuml-go

# Error handling is a bit lacking in the tool
# (https://github.com/yogendra/plantuml-go/issues/2).
# We work around that by checking the output.
%.png: %.puml $(DOC_PLANTUML_GO)
	$(DOC_PLANTUML_GO) -f png -o output $< >$@
	@ if ! file - <$@ | grep -q '^/dev/stdin: PNG image data'; then cat $@ >/dev/stderr; rm $@; false; fi

$(DOC_PLANTUML_GO):
	go get github.com/yogendra/plantuml-go
