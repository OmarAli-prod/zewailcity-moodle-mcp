---
name: zewail-moodle
description: Use when the user asks about their Zewail City Moodle (moodle.zewailcity.edu.eg) or university coursework - what's due, deadlines, assignments, submissions, grades, announcements, lecture slides, course files or readings, or studying from course material. Covers which moodle_* tools to call in what order, Zewail course codes, reading files, and the confirm-before-submit rule.
---

# Zewail City Moodle

The `moodle` MCP server acts as the student on `moodle.zewailcity.edu.eg` with their own permissions. All `moodle_*` tools come from it.

## If the tools are missing

If no `moodle_*` tools are available, the server isn't set up. Tell the user to run:

```bash
curl -fsSL https://raw.githubusercontent.com/OmarAli-prod/zewailcity-moodle-mcp/main/install.sh | sh
moodle auth
claude mcp add -s user moodle -- "$(command -v moodle-mcp)"
```

Then they restart Claude. If a tool returns `invalidtoken`, the token expired: they run `moodle auth` again.

## Ids

| Id | Where it comes from |
| --- | --- |
| `course_id` | `moodle_courses`, or the number in `course/view.php?id=…` |
| `cmid` | any activity in `moodle_course_contents`, or the number in `mod/<type>/view.php?id=…` |
| `assign_id` | `moodle_assignments`. `moodle_assignment` also accepts a `cmid` |
| `fileurl` | the `fileurl` field on any file another tool returned |

Zewail course shortnames look like `MATH104-FALL2026-LCTR02`: course code, term, section. Match the user's words ("linear algebra", "math 104", "the testing course") against both `shortname` and `fullname` from `moodle_courses`.

## Common requests

**"What's due?"** Call `moodle_deadlines` (add `days_back: 7` to include overdue). It only lists unfinished assignments and quizzes, and dates can lag teacher edits. For anything due soon, confirm with `moodle_assignment`, which also shows whether it's already submitted.

**"Help me study / summarize the slides."** `moodle_courses` → `moodle_course_contents` → pick the files → `moodle_file_text`. Weekly readings and "for next lecture" notes often sit in section `summary` text, not in files. Long files come back in slices: keep calling with `next_offset` until it's null. Cite the file name for each point.

**"What's new? / What did they upload?"** `moodle_whats_new` (default last 7 days) lists activities with new or replaced files, new forum posts and other content changes, with the current files attached. Follow up with `moodle_file_text` on anything the user wants summarised.

**"When are my lectures? / What's on this week?"** `moodle_calendar` shows everything on the calendar, including lecture attendance sessions and already-submitted due dates. Use it instead of `moodle_deadlines` for schedules.

**"Download my course files."** `moodle_sync` mirrors every course file into `~/Moodle/<course>/<section>/` and only fetches new or changed files on later runs. For one file use `moodle_download`.

**"How am I doing?"** `moodle_grades` with no arguments gives totals per course; with `course_id` it gives every item, weight and feedback. A grade of `-` means not graded yet.

**"What do I need on the final?"** Moodle reports every weight as `0.00 %` until items are graded, so take the weights from the course's syllabus or announcements (search with `moodle_search` or `moodle_announcements`), combine them with the graded scores from `moodle_grades`, and do the arithmetic yourself. Show the working.

**"Anything new?"** `moodle_announcements` for teacher posts, `moodle_notifications` (`unread_only: true`) for grades, feedback and reminders. Use `moodle_discussion` to open a full thread.

**Anything else** (calendar events, quiz reviews, lesson pages): `moodle_functions` with a keyword, then `moodle_call`.

The server also offers prompts (`whats_due`, `study_course`, `weekly_digest`) and resources (`moodle://courses`, `moodle://deadlines`, `moodle://course/{course_id}`).

## Writing to Moodle

`moodle_submit_assignment` replaces the student's real submission. Before calling it:

1. Show the exact text and the list of file paths that will be sent.
2. Get a clear yes.
3. Leave `submit_for_grading` false (it saves a draft) unless the user explicitly says to finalise. Finalising accepts the submission statement and usually can't be undone.

Treat `moodle_call` with any function that saves, submits, updates, deletes or sends the same way.

Before helping produce graded work, remind the user to check the course's AI policy if they haven't mentioned it.

## Presenting results

- Times come back already formatted in the user's local time (Cairo for most students). Don't convert them again.
- Lead with what needs action: overdue first, then the next 48 hours.
- Link items with the `url` field so the user can open them in Moodle.
