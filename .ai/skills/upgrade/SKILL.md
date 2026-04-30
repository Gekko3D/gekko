# Skill: Upgrade AI Scaffold

Use this skill when bringing the repo onto a newer AI scaffold version.

## Steps

1. Compare the incoming template version against `.ai/VERSION`.
2. Preserve repo-specific files and all content under `.ai.local/`.
3. Update shared framework files only where the new template adds value without fighting existing repo conventions.
4. Re-run the onboarding/refresh flow afterward to keep `.ai/docs/` and `.clinerules` aligned.

