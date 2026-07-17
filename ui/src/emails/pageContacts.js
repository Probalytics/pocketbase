import "./contactModal";
import { emailsSidebar } from "./emailsSidebar";

const STORAGE_KEY = "pbCrmContactsCollection";

export function pageContacts() {
    app.store.title = "Contacts";

    const data = store({
        collectionName: "",
        filter: "",
        sort: "",
        reset: null,
        get collection() {
            return app.store.collections.find((c) => c.name === data.collectionName);
        },
    });

    const contactCollections = () =>
        app.store.collections.filter(
            (c) => !c.system && (c.type === "auth" || c.fields.some((f) => f.name === "email")),
        );

    initCollection();

    function initCollection() {
        const available = contactCollections();
        const stored = window.localStorage.getItem(STORAGE_KEY);
        data.collectionName = available.find((c) => c.name === stored)?.name
            || available.find((c) => c.type === "auth")?.name
            || available[0]?.name
            || "";
    }

    function selectCollection(name) {
        data.collectionName = name;
        window.localStorage.setItem(STORAGE_KEY, name);
        data.filter = "";
        data.sort = "";
    }

    function refresh() {
        data.reset = Date.now();
    }

    return t.div(
        { pbEvent: "pageContacts", className: "page page-contacts" },
        emailsSidebar(),
        t.div(
            { className: "page-content full-height" },
            t.header(
                { className: "page-header flex-nowrap" },
                t.nav(
                    { className: "breadcrumbs" },
                    t.div(null, "CRM"),
                    t.div(null, "Contacts"),
                ),
                t.div(
                    { className: "page-header-secondary-btns" },
                    t.div(
                        { className: "contacts-collection-select" },
                        app.components.select({
                            options: () => contactCollections().map((c) => ({ value: c.name, label: c.name })),
                            value: () => data.collectionName,
                            onchange: (s) => selectCollection(s?.[0]?.value || ""),
                        }),
                    ),
                    app.components.refreshButton({
                        onclick: refresh,
                        className: "btn transparent circle rotate-btn secondary",
                        tooltip: () => "Refresh",
                    }),
                ),
            ),
            t.div(
                {
                    hidden: () => !!data.collectionName,
                    className: "block txt-center p-base",
                },
                t.h6(
                    { className: "txt" },
                    "No contact collection found. Add an auth collection or one with an email field.",
                ),
            ),
            app.components.recordsSearchbar({
                hidden: () => !data.collection,
                collection: () => data.collection,
                value: () => data.filter,
                onsubmit: (newFilter) => (data.filter = newFilter),
            }),
            () => {
                if (!data.collection) {
                    return;
                }
                return app.components.recordsList({
                    className: "m-t-sm",
                    reset: () => data.reset,
                    collection: () => data.collection,
                    filter: () => data.filter,
                    sort: () => data.sort,
                    onselect: (record) => app.modals.openContact(data.collection, record),
                    onchange: (newFilter, newSort) => {
                        data.filter = newFilter;
                        data.sort = newSort;
                    },
                });
            },
            t.footer({ className: "page-footer" }, app.components.credits()),
        ),
    );
}
