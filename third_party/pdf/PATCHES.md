# Local PDF compatibility patches

Based on `github.com/ledongthuc/pdf` commit `6c8c28e0e8a0` (2026-09-07), under the original BSD license. The root Go module replaces this dependency with this directory. Runtime code remains Go only.

- `read.go`: truncate object encryption keys to `min(file key bytes + 5, 16)` as required by the PDF encryption algorithm. Fixes 40-bit RC4 HSBC statements.
- `ps.go`: skip inline binary images instead of passing sample bytes through the PDF token lexer. Unfiltered samples use dimensions and component depth; filtered images scan for the whitespace-delimited end marker.
- `page.go`: provide standard core-font widths, handle Type0 CID widths and advance two-byte character codes correctly; include word spacing and remove synthetic newlines from positioned text.
- `core_widths.go`: Adobe core-font metrics copied from gofpdf's MIT-licensed font JSON files. See `LICENSE-font-metrics`.

The application regression test parses all 19 bundled HSBC statements and compares 96 transaction IDs, dates, account/currency fields and monetary values against the existing database. The existing original files are only read; tests use temporary data copies. Remaining parser limitations include arbitrary forms/XObjects, unusual encodings, rotated or modified layouts, and PDFs requiring nonempty passwords. Failed or unsupported bank statements are rejected, not accepted as zero transactions.
