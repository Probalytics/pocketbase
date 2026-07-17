import { emailsSidebar } from "./emailsSidebar";

// emailsListLayout renders a full-width native page shell (matching the
// Collections screen): the CRM sidebar, a page-header with breadcrumbs and an
// optional primary action, the full-width body, and the standard page footer.
export function emailsListLayout(breadcrumbs, actions, body) {
    return t.div(
        { className: "page page-crm" },
        emailsSidebar(),
        t.div(
            { className: "page-content full-height" },
            t.header(
                { className: "page-header flex-nowrap" },
                t.nav({ className: "breadcrumbs" }, ...breadcrumbs.map((crumb) => t.div(null, crumb))),
                actions ? t.div({ className: "page-header-primary-btns" }, actions) : undefined,
            ),
            body,
            t.footer({ className: "page-footer" }, app.components.credits()),
        ),
    );
}

// emailsLayout renders the shared marketing page shell: the section sidebar,
// a breadcrumbs header with optional actions, and the page body.
export function emailsLayout(breadcrumbs, body, actions) {
    return t.div(
        { className: "page page-emails" },
        emailsSidebar(),
        t.div(
            { className: "page-content full-height" },
            t.header(
                { className: "page-header" },
                t.nav(
                    { className: "breadcrumbs" },
                    ...breadcrumbs.map((crumb) =>
                        crumb.href
                            ? t.a({ className: "breadcrumb-item", href: crumb.href }, crumb.label)
                            : t.div({ className: "breadcrumb-item" }, crumb.label)
                    ),
                ),
                actions ? t.div({ className: "inline-flex gap-5 m-l-auto" }, actions) : undefined,
            ),
            t.div({ className: "wrapper m-b-base" }, body),
        ),
    );
}

const campaignStatusClasses = {
    draft: "label",
    scheduled: "label warning",
    sending: "label info",
    sent: "label success",
    paused: "label warning",
    failed: "label danger",
    canceled: "label",
};

export function statusLabel(status) {
    return t.span({ className: campaignStatusClasses[status] || "label" }, status || "draft");
}

export function formatDate(value) {
    return value ? value.substring(0, 16).replace("T", " ") : "-";
}
