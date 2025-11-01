Contributing
------------

Some coding guidelines:
- Write safe Go code: use `context` when channels are involved, use locking when concurrency is involved.
- Don’t create abstractions for **future** maintainability. Everything you make will be used, or else there is no reason for the code to exist.
- Use tree-sitter for complex document structures such as php or twig. Use static analysis lookups in the `internal/php` package if types or function return values are relevant.
- LSP features should be implemented through the `internal/analyzer` package. The server asks the analyzer if it knows the requested feature, and the analyzer responds.

Your PR will:
- Mention what is in your PR
- Explain why the change is required
- Optionally give a step-by-step example of how to take advantage of your new code.

Your Issue will:
- Explain the current behavior
- Explain the expected behavior
- Optionally suggest a possible fix
