// Keep archived probe sources out of the repository's recursive package scan.
// Their recorded builds use explicit files from the frozen RCC module cwd.
module rcc-successor-evidence

go 1.26.9
