# Intl API: Go-Native Subset vs Full ICU

The ECMA-402 Intl API specifies 11 constructors backed by ICU (International Components for Unicode), a 30MB C/C++ library. Full Intl requires Unicode collation, CLDR plural rules, text segmentation, and locale data.

We decided to implement a Go-native subset ("Intl Lite") covering 5 constructors (getCanonicalLocales, NumberFormat, DateTimeFormat, ListFormat, RelativeTimeFormat) using Go standard library and `golang.org/x/text` packages. This covers ~80% of real-world Intl usage without CGO, binary bloat, or external data files.

Considered alternatives: full ICU via CGO (rejected — defeats V8Go's lightweight, portable, pure-Go advantage), implementing all 11 constructors in pure Go (rejected — requires thousands of lines of Unicode algorithm implementations), skipping Intl entirely (rejected — Intl is used in modern JS for date/number formatting).

Excluded constructors (Collator, PluralRules, Segmenter, DisplayNames, Locale, DurationFormat) require ICU-level CLDR data not available in Go's standard library.
