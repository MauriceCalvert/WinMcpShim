# WinMcpShim TODO

1. ~~UTF-16 read-only decode path (read, head, tail, cat, wc, diff)~~ DONE — `TestRead_UTF16LEDecode`, `TestRead_UTF16BEDecode`, `TestHead_UTF16`, `TestTail_UTF16`, `TestCat_UTF16`, `TestWc_UTF16`, `TestDiff_UTF16`, `TestWrite_AfterUTF16Read`, `TestEdit_RefusesUTF16`
2. ~~Built-in grep fallback (Go regexp + WalkDir)~~ DONE — 36 unit tests in `tools/grep_test.go` (`TestGrep_SingleFileMatch`, `TestGrep_RecursiveSearch`, etc.)
3. ~~Include allowed roots in confinement error messages~~ DONE — `TestCriticalError_ConfinementFormat`
4. ~~Cap stderr buffer at MaxOutput~~ DONE — `TestRun_StderrTruncated`, `TestRun_StderrUnderLimit`, `TestRun_BothTruncated`

All items complete.
