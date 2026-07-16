const emailsNavLinks = [
    { href: "#/emails/campaigns", icon: "ri-mail-send-line", label: "Campaigns" },
    { href: "#/emails/automations", icon: "ri-flow-chart", label: "Automations" },
    { href: "#/emails/templates", icon: "ri-layout-2-line", label: "Templates" },
    { href: "#/emails/suppressions", icon: "ri-forbid-2-line", label: "Suppressions" },
];

export function emailsSidebar() {
    return app.components.pageSidebar(
        { pbEvent: "emailsSidebar", className: "settings-sidebar" },
        t.nav(
            { className: "sidebar-content scrollable" },
            t.details(
                { className: "nav-group", open: true },
                t.summary(
                    { tabIndex: -1, onfocusout: () => false, onclick: () => false, onkeyup: () => false },
                    "Marketing",
                ),
                ...emailsNavLinks.map((link) =>
                    t.a(
                        {
                            href: link.href,
                            className: () => `nav-item ${app.utils.isActivePath(link.href, false) ? "active" : ""}`,
                        },
                        t.i({ className: link.icon, ariaHidden: true }),
                        t.span({ className: "txt" }, link.label),
                    )
                ),
            ),
        ),
    );
}
