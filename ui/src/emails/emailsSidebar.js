const crmNavLinks = [
    { href: "#/crm/contacts", icon: "ri-contacts-book-2-line", label: "Contacts" },
    { href: "#/crm/audiences", icon: "ri-group-line", label: "Audiences" },
    { href: "#/crm/campaigns", icon: "ri-mail-send-line", label: "Campaigns" },
    { href: "#/crm/automations", icon: "ri-flow-chart", label: "Automations" },
    { href: "#/crm/templates", icon: "ri-layout-2-line", label: "Templates" },
    { href: "#/crm/suppressions", icon: "ri-forbid-2-line", label: "Suppressions" },
];

export function emailsSidebar() {
    return app.components.pageSidebar(
        { pbEvent: "crmSidebar", className: "settings-sidebar" },
        t.nav(
            { className: "sidebar-content scrollable" },
            t.details(
                { className: "nav-group", open: true },
                t.summary(
                    { tabIndex: -1, onfocusout: () => false, onclick: () => false, onkeyup: () => false },
                    "CRM",
                ),
                ...crmNavLinks.map((link) =>
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
