import { contactAvatar } from "./contactModal";
import { emailsLayout, formatDate } from "./emailsLayout";

const STORAGE_KEY = "pbCrmContactsCollection";
const PER_PAGE = 40;

export function pageContacts() {
    app.store.title = "Contacts";

    const data = store({
        collectionName: "",
        search: "",
        contacts: [],
        suppressed: new Set(),
        page: 1,
        totalPages: 1,
        totalItems: 0,
        isLoading: true,
        get collection() {
            return app.store.collections.find((c) => c.name === data.collectionName);
        },
    });

    const contactCollections = () =>
        app.store.collections.filter(
            (c) => !c.system && (c.type === "auth" || c.fields.some((f) => f.name === "email")),
        );

    initCollection();
    loadSuppressions();

    function initCollection() {
        const available = contactCollections();
        const stored = window.localStorage.getItem(STORAGE_KEY);
        data.collectionName = available.find((c) => c.name === stored)?.name
            || available.find((c) => c.type === "auth")?.name
            || available[0]?.name
            || "";
        if (data.collectionName) load(1);
    }

    function selectCollection(name) {
        data.collectionName = name;
        window.localStorage.setItem(STORAGE_KEY, name);
        load(1);
    }

    let searchTimer;
    function onSearch(value) {
        data.search = value;
        clearTimeout(searchTimer);
        searchTimer = setTimeout(() => load(1), 250);
    }

    function hasField(name) {
        return !!data.collection?.fields.some((f) => f.name === name);
    }

    async function load(page) {
        if (!data.collectionName) return;
        data.isLoading = page === 1;
        try {
            const res = await app.pb.collection(data.collectionName).getList(page, PER_PAGE, {
                filter: buildFilter(),
                sort: hasField("created") ? "-created" : "-id",
            });
            data.contacts = page === 1 ? res.items : data.contacts.concat(res.items);
            data.page = res.page;
            data.totalPages = res.totalPages;
            data.totalItems = res.totalItems;
        } catch (err) {
            if (!err.isAbort) app.checkApiError(err);
        }
        data.isLoading = false;
    }

    function buildFilter() {
        const term = data.search.trim().replace(/"/g, "\\\"");
        if (!term) return "";
        const hasName = data.collection?.fields.some((f) => f.name === "name");
        return hasName ? `email ~ "${term}" || name ~ "${term}"` : `email ~ "${term}"`;
    }

    async function loadSuppressions() {
        try {
            const items = await app.pb.collection("_mailSuppressions").getFullList({ fields: "email" });
            data.suppressed = new Set(items.map((i) => i.email));
        } catch (_) {}
    }

    return emailsLayout([{ label: "Contacts" }], () => {
        if (!data.collectionName) {
            return t.div(
                { className: "txt-hint txt-center p-base" },
                "No contact collection found. Create a collection with an email field or an auth collection.",
            );
        }

        return t.div(
            { className: "block" },
            t.div(
                { className: "flex gap-10 m-b-base" },
                t.input({
                    type: "text",
                    className: "flex-fill",
                    placeholder: "Search contacts by name or email...",
                    value: () => data.search,
                    oninput: (e) => onSearch(e.target.value),
                }),
                t.div(
                    { style: "min-width:180px" },
                    app.components.select({
                        options: () => contactCollections().map((c) => ({ value: c.name, label: c.name })),
                        value: () => data.collectionName,
                        onchange: (s) => selectCollection(s?.[0]?.value || ""),
                    }),
                ),
            ),
            () => {
                if (data.isLoading) {
                    return t.div({ className: "block txt-center p-base" }, t.span({ className: "loader lg" }));
                }
                if (!data.contacts.length) {
                    return t.div({ className: "txt-hint txt-center p-base" }, "No contacts found.");
                }
                return contactsTable();
            },
        );
    }, t.span({ className: "txt-hint" }, () => `${data.totalItems} contacts`));

    function contactsTable() {
        return t.div(
            null,
            t.table(
                { className: "table" },
                t.thead(null, t.tr(null, t.th(null, "Contact"), t.th(null, "Status"), t.th(null, "Added"))),
                t.tbody(
                    null,
                    ...data.contacts.map((contact) =>
                        t.tr(
                            {
                                className: "row-handle",
                                tabIndex: 0,
                                onclick: () => app.modals.openContact(data.collection, contact),
                            },
                            t.td(
                                null,
                                t.div(
                                    { className: "contact-row" },
                                    contactAvatar(contact),
                                    t.div(
                                        null,
                                        t.div({ className: "contact-name" }, contact.name || contact.email),
                                        contact.name ? t.div({ className: "contact-email" }, contact.email) : undefined,
                                    ),
                                ),
                            ),
                            t.td(
                                null,
                                data.suppressed.has(contact.email)
                                    ? t.span({ className: "label danger" }, "Unsubscribed")
                                    : t.span({ className: "label success" }, "Subscribed"),
                            ),
                            t.td({ className: "txt-hint" }, formatDate(contact.created)),
                        )
                    ),
                ),
            ),
            data.page < data.totalPages
                ? t.div(
                    { className: "block txt-center m-t-sm" },
                    t.button(
                        { type: "button", className: "btn secondary", onclick: () => load(data.page + 1) },
                        t.span({ className: "txt" }, "Load more"),
                    ),
                )
                : undefined,
        );
    }
}
