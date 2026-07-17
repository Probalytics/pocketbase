// filterField renders PocketBase's native "pbrule" filter editor (the same
// one used by API rules and record search) with field autocomplete for the
// collection returned by collectionGetter (a name resolved live, so the
// suggestions follow the currently selected collection).
export function filterField(label, collectionGetter, getValue, setValue, placeholder) {
    const autocomplete = (word) => {
        const collection = app.store.collections.find((c) => c.name === collectionGetter());
        if (!collection) {
            return [];
        }
        return app.utils.collectionAutocompleteKeys(collection, word, {
            requestKeys: false,
            collectionJoinKeys: false,
        });
    };

    return t.div(
        { className: "field rule-field" },
        t.label(
            null,
            t.i({ className: "ri-filter-3-line", ariaHidden: true }),
            t.span({ className: "txt" }, label),
        ),
        (el) =>
            app.components.codeEditor({
                language: "pbrule",
                value: getValue,
                oninput: setValue,
                placeholder: () => placeholder || "Leave empty to include everyone...",
                autocomplete,
                autocompleteContainer: el,
            }),
    );
}
