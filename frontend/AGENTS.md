# Frontend Component Boundaries

- `src/components/ui/` contains style-only primitives. Do not add product copy, data fetching, or business rules there.
- `src/components/shared/` contains reusable application components and hooks shared by multiple product areas.
- Feature directories such as `chat/`, `admin/`, `settings/`, and `storage/` contain domain-specific components.
- Application code must use `Button` instead of native `<button>` elements and `cn` instead of importing `clsx` or `tailwind-merge` directly.
- Keep visual behavior consistent with the existing Base UI and Tailwind design system. Extend a shared primitive before copying long class strings into feature code.
