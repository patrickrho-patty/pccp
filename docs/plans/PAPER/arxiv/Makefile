LATEXMK := latexmk
EN_TARGET := PAPER_arXiv
KO_TARGET := PAPER_arXiv_KO
ARCHIVE := PAPER-arXiv-source.tar.gz
COMMON_DEPS := references.bib $(wildcard figures/*) benchmark-data/core-primitives.tsv

.PHONY: all verify check clean archive

all: $(EN_TARGET).pdf $(KO_TARGET).pdf

$(EN_TARGET).pdf: main.tex $(COMMON_DEPS)
	$(LATEXMK) -pdf -interaction=nonstopmode -halt-on-error -file-line-error -jobname=$(EN_TARGET) main.tex

$(KO_TARGET).pdf: main_ko.tex $(COMMON_DEPS)
	$(LATEXMK) -xelatex -interaction=nonstopmode -halt-on-error -file-line-error -jobname=$(KO_TARGET) main_ko.tex

verify: all
	@test -s $(EN_TARGET).pdf
	@test -s $(KO_TARGET).pdf
	@! grep -E 'Undefined control sequence|Citation .* undefined|Reference .* undefined|There were undefined references|Overfull \\hbox' $(EN_TARGET).log $(KO_TARGET).log

check: verify

clean:
	$(LATEXMK) -C -jobname=$(EN_TARGET) main.tex
	$(LATEXMK) -C -xelatex -jobname=$(KO_TARGET) main_ko.tex

archive: verify
	COPYFILE_DISABLE=1 tar -czf $(ARCHIVE) --options gzip:!timestamp main.tex main_ko.tex references.bib Makefile README.md implementation-claim-matrix.tsv figures benchmark-data
