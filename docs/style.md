# Writing style

Shared verbatim across raider-mate-service, raider-mate-bot, and raider-mate-dashboard.
If you edit this file, copy the same edit to the other two repos.

Everything a human reads (code comments, doc comments, commit messages, README, error
strings, PR descriptions) must not read as machine-generated.

This is not cosmetic. This is a public AGPL repository. Generated-sounding prose
signals low effort and costs contributor trust before anyone reads the code.

The top rules are inlined in each repo's `AGENTS.md`. This file is the full set, read
when writing docs, comments, or anything else prose-heavy.

## Punctuation and structure

- **No em dashes.** Use a comma, a colon, parentheses, or start a new sentence.
- **No litanies.** Avoid the rhythmic triple: "fast, reliable, and scalable", "clean,
  maintainable, and testable". Say the one thing that is true and useful.
- **No "not just X, but Y"** and no "it's not about X, it's about Y".
- **No rhetorical question openers.** "So what does this mean?" Delete it.
- **No bold-lead bullet lists where prose works.** A paragraph is often correct.
- **No emoji** in code, comments, commits, or logs.
- **Vary sentence length.** Machine prose is uniformly medium-length. Short sentences
  are allowed. So are long ones that actually need the room.
- **No summary paragraph** restating what was just said.

## Banned vocabulary

```
delve            leverage (verb)   robust           seamless
comprehensive    elegant           powerful         cutting-edge
best-in-class    crucial           vital            ensure that
it's worth noting               it's important to note
in today's world                dive into
unlock           empower           streamline       holistic
journey          landscape (figurative)             tapestry
testament to     realm of          navigate (figurative)
```

Say the concrete thing instead. Not "ensures robust error handling" but "returns an
error if the realm is unknown".

## Comments

- **Comment why, not what.** `// increment counter` above `counter++` is noise.
  `// Raider.IO rate-limits at 300/min, so batch in 50s` is worth having.
- **No section banners.** No `// ===== Helpers =====`, no `// --- Types ---`.
- **No doc comments on obvious functions.** A doc comment on `func (r Roster) Len()
  int` is padding.
- **Go doc comments start with the identifier name**, one sentence where possible:
  `// SyncCharacter refreshes cached gear from Raider.IO.`
- **No TODO without an owner and a reason.** Either fix it or open an issue.
- **No comments narrating the diff.** `// Added validation here` is meaningless three
  commits later.
- **Domain comments should sound like a raider wrote them.** "Two tanks unless it's a
  Council fight" beats "The tank requirement is configurable per encounter type." It is
  shorter, more accurate, and signals that the codebase was built by someone who plays
  the game.

## Commit messages

- Imperative mood, lowercase subject, no trailing period: `add signup deadline job`
- Body explains why, not a bulleted summary of the diff.
- No self-congratulation. Never "successfully implemented a robust solution for".
- No `Generated with` or `Co-Authored-By` trailers unless explicitly asked. They are
  visible forever in a public repository's history.

## Error strings

Go convention: lowercase, no punctuation, no capitalisation, since they get wrapped.

```go
// good
return fmt.Errorf("character %s not found on realm %s", name, realm)

// bad
return fmt.Errorf("Error: Failed to retrieve the character. Please try again.")
```

User-facing Discord messages are different: those are read by players, so they should
be short, plain, and occasionally have a sense of humour. Never apologetic boilerplate.

## Code smells that read as machine-written

- Defensive nil checks for conditions that cannot occur.
- Wrapping every call in error handling that only re-returns the error unchanged.
- Generic names where a domain noun exists: `data`, `result`, `item`, `helper`,
  `utils`, `manager`, `processor`, `handler`. Prefer `roster`, `signup`, `assignment`.
- `any` or `interface{}` used to avoid thinking about the type.
- Configuration options with exactly one possible value.
- Test names like `TestFunctionWorks`. Describe the case:
  `TestBenchOverflowOrdersBySignupTime`.
- Symmetrical structure imposed where the domain is not symmetrical.
- Every function the same length.

## The test

Read it aloud. If it sounds like a product landing page, a LinkedIn post, or a
technical writer who has never played WoW, rewrite it.

## Why this matters here specifically

The author is learning Go on this project. Generated prose and generated-looking code
both hide the line between what was understood and what was pattern-matched. Writing
that sounds like a person is also writing that a person checked.
