# Mason Project Description

This is a brand new project.  The work we do from here on is the foundational work for this project.  This will be a Go project, potentionally with at least a couple parts to it (local, remote, grammars, etc.).  Mason will be a utility whose purpose will be the making it trivially easy to move file assets to a remote server.

## Why?

Aren't there tons of utilities that push bits around?  Yes, there are!  And we might even end up using them!  Maybe we'll discover tools along the way that already do what Mason will do - but better - and so Mason will become irrelevant.  That's an acceptable outcome from this work.

I have a VPS that I use to host static content.  Think things like static web sites, video and audio files, etc.  It's pretty easy to get content onto the server - I can just go to a directory, build a thing, then `rsync` the files.  And then I have to... remember the credentials.  Remember the paths to the remote host.  Remember the user and the hostname.  Remember the password.  The rsync flags, etc.  Lots to remember.  I do believe that solutions to this problem should be easier to puzzle over and to implement.

The first thought I had was to simply place a few commands in a Makefile and let some essentially advanced aliasing take over.  This means, however, that each project has to implement much of the same scripting.  If anything changes about my remote servers, any Makefile that references that configuration will be out of date, needing to be fixed.  

What I'd ideally like instead is a program that treats common use cases as single commands.  A project might have a directory in it that's the build target of a static build tool - storing a .mason file in the project root should hold all the context and config necessary to just one-shot the thing.  That could even mean stored SSH params - I know and accept a certain degree of risk here.

A `.mason` configuration file could be toml or json or whatever - json or yaml is more readily understandable than what toml for me, but if toml is the most accessible from go, choose the one that makes the most sense.

Other workflows could be a little more complicated.  An upload might not land on a particular site as part of "generated content."  I'd like to have the ability to link to content through a shortlink mechanism.  Let's say that I've got an mp3 recording that I'd like to share outside of Messenger / Whatsapp / iMessage.  I'd ideally like to be able to go to the directory where such content is stored, run the command to "share" a file - then have it uploaded and see a shareable link returned to me.  Something like https://static.aaronware.com/b3c.  The destination name is not really the important part - the destination name shown above would be configurable, and the expectation is that the above is acommplished (the `b3c` link to the content) completely transparently but just something something like:

```
mason share /user/aaron/Recordings/2026-06-05-190432.mp3
```

or whatever - you get the point.

Projects that have been had `mason init` run in them will have a mason file.  The mason file will allow for easy, pre-scripted commands to run very simply.  If no .mason file is found in the current project, then the program should look for mason files in a few more sensible places, like the user home directory.

ALSO - I'm not completely sold on mason.  If you think mason could work, that's fine.  It does type quickly.  The connection with mason is that a mason will assemble blocks to make something useful, even beautiful.  The mason application would assemble files on behalf of others to... make the publishing of content super straightforward.

I want to pause here and take a moment and see where you are in understanding

---

## Documentation

Planning continued in a chain of documents. Read them in order when catching up on design decisions:

| Step | Document | Role |
|------|----------|------|
| 1 | [PROJECT_DESCRIPTION.md](./PROJECT_DESCRIPTION.md) | Motivation and original vision *(you are here)* |
| 2 | [DESIGN.md](./DESIGN.md) | First-pass architecture — retained as historical record |
| 3 | [DESIGN_REFINEMENT.md](./DESIGN_REFINEMENT.md) | Revised architecture; supersedes DESIGN.md where they conflict |
| 4 | [COMMANDS.md](./COMMANDS.md) | Authoritative CLI specification |
| 5 | [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md) | **Implementation guide, work plan, and contextq context** |

**Precedence for implementation:** [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md) → [COMMANDS.md](./COMMANDS.md) → [DESIGN_REFINEMENT.md](./DESIGN_REFINEMENT.md) → [DESIGN.md](./DESIGN.md) → this file.

Implementation work is tracked with [contextq](https://github.com/norlinga/contextq) ([docs](https://contextq.longship.dev)).