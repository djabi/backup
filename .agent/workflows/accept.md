---
description: Run before accepting task completion
---

Pre-Commit Checklist

Code Quality & Testing:
- Run lint checks on all modified files.
- Ensure all existing tests pass.
- Write new tests to cover any fragile changes to prevent future regressions.

Review & Documentation:
- Perform a meticulous code review, paying close attention to multi-platform support, and address any identified issues.
- Execute meticulous documentation updates. Update the CHANGELOG if necessary.

Final Checks:
- Run all tests a final time.
- Review the modified files to ensure no unnecessary or temporary files are staged for commit.
- Update the tool version if warranted by the staged changes.
