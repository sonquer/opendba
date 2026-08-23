# Changelog

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Work towards the first release: a terminal database workbench for PostgreSQL and
SQLite, read only by default. The interface fills the terminal, scrolls, and is
driven by a command palette on `ctrl+k` as well as by keys, which are shown the
way a Mac keyboard prints them. The editor sits beside the schema it is written
against, results fill their pane and zoom to the window, and leaving asks
first. Connections are set up, switched and removed
inside the interface, passwords go to the keychain, and every statement is
classified against the real grammar of the target database before it is sent.

### The assistant

`a` opens a conversation about the database in front of you, and carries any
page opened with `enter` into it. The assistant has read only tools and uses
them rather than guessing, every statement it wants to run is classified by the
same guard a typed statement meets, and everything a tool returns is data rather
than instructions however it is worded.

Six back-ends behind one interface: a model running inside this process through
llama.cpp, the hosted ones from Anthropic, OpenAI and Google, an Ollama daemon,
and anything else that answers chat completions.

Nothing has to be configured before the first question. Pressing `a` with an
empty `[ai]` section opens a list of everything that could answer, grouped by
provider with the models that run on this machine at the top. Choosing a model
downloads it and starts using it; choosing a provider asks for a key and keeps
it in the keychain, writing only a reference to it. A key already in the
environment is offered as it stands. `ctrl+o` opens the same list from inside a
conversation.

A statement the assistant writes is put on the screen before it runs, and runs
when the person says so. It has already met the same classifier a typed
statement meets, so the question is whether to read that rather than whether it
is safe, and it is what lets the assistant answer anything it can write a query
for.

Before the first request of a turn that would leave this machine, the screen
says what would be sent and waits. It says it by class — your question, the
shape of the database, rows out of the tables — and asks again when a turn would
add a class you have not allowed. A model running here is not asked about,
because nothing leaves.

Local models are downloaded from Hugging Face with the download resumed if it is
interrupted and the weights checked before they are given a name. While one
arrives the list gives way to it, and leaving asks before it is given up on.

llama.cpp is not downloaded: this program carries the build it was written
against for every platform it is published for, and writes it out the first time
a conversation is opened. Nothing about the inference library is fetched at run
time.

[Unreleased]: https://github.com/sonquer/tui4db/commits/main
