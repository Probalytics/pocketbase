import { emailsLayout } from "./emailsLayout";

const COLLECTION = "_mailAutomations";

const delayUnits = [
    { label: "minutes", seconds: 60 },
    { label: "hours", seconds: 3600 },
    { label: "days", seconds: 86400 },
];

export function pageAutomation(route) {
    const isNew = route.params.id === "new";

    const data = store({
        isLoading: !isNew,
        isSaving: false,
        automation: blankAutomation(),
    });

    app.store.title = isNew ? "New automation" : "Automation";

    if (!isNew) load();

    async function load() {
        data.isLoading = true;
        try {
            const record = await app.pb.collection(COLLECTION).getOne(route.params.id);
            record.steps = Array.isArray(record.steps) ? record.steps : [];
            data.automation = record;
            app.store.title = record.name || "Automation";
        } catch (err) {
            if (!err.isAbort) app.checkApiError(err);
        }
        data.isLoading = false;
    }

    async function save() {
        if (data.isSaving) return;
        data.isSaving = true;
        try {
            const payload = {
                name: data.automation.name,
                enabled: !!data.automation.enabled,
                triggerCollection: data.automation.triggerCollection || "",
                triggerEvent: data.automation.triggerEvent || "create",
                triggerCondition: data.automation.triggerCondition || "",
                exitCondition: data.automation.exitCondition || "",
                steps: data.automation.steps || [],
            };
            data.automation = data.automation.id
                ? await app.pb.collection(COLLECTION).update(data.automation.id, payload)
                : await app.pb.collection(COLLECTION).create(payload);
            data.automation.steps = Array.isArray(data.automation.steps) ? data.automation.steps : [];

            app.toasts.success("Automation saved.");
            if (isNew) window.location.hash = "#/emails/automations/" + data.automation.id;
        } catch (err) {
            app.checkApiError(err);
        }
        data.isSaving = false;
    }

    function addStep(kind) {
        data.automation.steps = data.automation.steps.concat(
            kind === "delay"
                ? { kind: "delay", seconds: 86400 }
                : { kind: "email", subject: "", body: "", template: "" },
        );
    }
    function removeStep(index) {
        data.automation.steps = data.automation.steps.filter((_, i) => i !== index);
    }
    function moveStep(index, delta) {
        const steps = data.automation.steps.slice();
        const target = index + delta;
        if (target < 0 || target >= steps.length) return;
        [steps[index], steps[target]] = [steps[target], steps[index]];
        data.automation.steps = steps;
    }

    const triggerCollections = () =>
        app.store.collections.filter((c) => !c.system && (c.type === "auth" || c.type === "base"));

    return emailsLayout(
        [{ label: "Automations", href: "#/emails/automations" }, { label: () => app.store.title }],
        () => {
            if (data.isLoading) {
                return t.div({ className: "block txt-center" }, t.span({ className: "loader lg" }));
            }
            return t.div(
                { className: "grid" },
                t.div({ className: "col-lg-5" }, t.div({ className: "grid" }, ...triggerFields())),
                t.div({ className: "col-lg-7" }, pipelinePanel()),
            );
        },
        t.button(
            { type: "button", className: () => `btn sm expanded ${data.isSaving ? "loading" : ""}`, onclick: save },
            t.span({ className: "txt" }, "Save"),
        ),
    );

    function triggerFields() {
        return [
            cell(field("Name", () =>
                t.input({
                    type: "text",
                    value: () => data.automation.name || "",
                    oninput: (e) => (data.automation.name = e.target.value),
                }))),
            cell(t.div(
                { className: "field" },
                t.input({
                    id: "automation_enabled",
                    type: "checkbox",
                    className: "switch",
                    checked: () => !!data.automation.enabled,
                    onchange: (e) => (data.automation.enabled = e.target.checked),
                }),
                t.label({ htmlFor: "automation_enabled" }, t.span({ className: "txt" }, "Enabled")),
            )),
            cell(field("Trigger collection", () =>
                app.components.select({
                    placeholder: "Select collection",
                    options: () => triggerCollections().map((c) => ({ value: c.name, label: c.name })),
                    value: () => data.automation.triggerCollection || "",
                    onchange: (s) => (data.automation.triggerCollection = s?.[0]?.value || ""),
                }))),
            cell(field("Trigger event", () =>
                app.components.select({
                    options: () => [
                        { value: "create", label: "Record created" },
                        { value: "update", label: "Record updated" },
                    ],
                    value: () => data.automation.triggerEvent || "create",
                    onchange: (s) => (data.automation.triggerEvent = s?.[0]?.value || "create"),
                }))),
            cell(field("Trigger condition (optional)", () =>
                t.textarea({
                    rows: 2,
                    className: "txt-mono",
                    placeholder: "plan='pro'",
                    value: () => data.automation.triggerCondition || "",
                    oninput: (e) => (data.automation.triggerCondition = e.target.value),
                }))),
            cell(field("Exit condition (optional)", () =>
                t.textarea({
                    rows: 2,
                    className: "txt-mono",
                    placeholder: "unsubscribed=true — stops the pipeline early",
                    value: () => data.automation.exitCondition || "",
                    oninput: (e) => (data.automation.exitCondition = e.target.value),
                }))),
        ];
    }

    function pipelinePanel() {
        return t.div(
            null,
            t.div({ className: "txt-bold m-b-sm" }, "Pipeline"),
            () => {
                if (!data.automation.steps.length) {
                    return t.div({ className: "txt-hint p-base" }, "No steps yet. Add an email or a delay below.");
                }
                return t.div(null, ...data.automation.steps.map((step, index) => stepCard(step, index)));
            },
            t.div(
                { className: "flex gap-10 m-t-base" },
                t.button(
                    { type: "button", className: "btn sm secondary", onclick: () => addStep("email") },
                    t.i({ className: "ri-mail-add-line" }),
                    t.span({ className: "txt" }, "Add email"),
                ),
                t.button(
                    { type: "button", className: "btn sm secondary transparent", onclick: () => addStep("delay") },
                    t.i({ className: "ri-time-line" }),
                    t.span({ className: "txt" }, "Add delay"),
                ),
            ),
        );
    }

    function stepCard(step, index) {
        return t.div(
            { className: "m-b-sm" },
            index > 0 ? t.div({ className: "email-flow-connector" }) : undefined,
            t.div(
                { className: () => `email-step ${step.kind === "delay" ? "email-step-delay" : ""}` },
                t.div(
                    { className: "flex gap-10 m-b-sm" },
                    t.i({ className: step.kind === "delay" ? "ri-time-line" : "ri-mail-line" }),
                    t.strong(null, step.kind === "delay" ? "Wait" : "Email"),
                    t.div(
                        { className: "inline-flex gap-5 m-l-auto" },
                        iconBtn("ri-arrow-up-line", () => moveStep(index, -1)),
                        iconBtn("ri-arrow-down-line", () => moveStep(index, 1)),
                        iconBtn("ri-delete-bin-line", () => removeStep(index)),
                    ),
                ),
                step.kind === "delay" ? delayFields(step) : emailFields(step),
            ),
        );
    }

    function delayFields(step) {
        return t.div(
            { className: "flex gap-10" },
            t.input({
                type: "number",
                min: 1,
                style: "max-width:100px",
                value: () => stepDelayValue(step),
                oninput: (e) => setStepDelay(step, parseInt(e.target.value, 10)),
            }),
            app.components.select({
                options: () => delayUnits.map((u) => ({ value: "" + u.seconds, label: u.label })),
                value: () => "" + stepDelayUnit(step),
                onchange: (s) => setStepUnit(step, parseInt(s?.[0]?.value, 10)),
            }),
        );
    }

    function emailFields(step) {
        return t.div(
            null,
            t.div(
                { className: "field m-b-sm" },
                t.input({
                    type: "text",
                    placeholder: "Subject",
                    value: () => step.subject || "",
                    oninput: (e) => (step.subject = e.target.value),
                }),
            ),
            t.div(
                { className: "field" },
                t.textarea({
                    rows: 4,
                    className: "txt-mono",
                    placeholder: "<p>Hi {RECORD:name}</p>",
                    value: () => step.body || "",
                    oninput: (e) => (step.body = e.target.value),
                }),
            ),
        );
    }
}

function stepDelayUnit(step) {
    const seconds = step.seconds || 0;
    for (const unit of [86400, 3600, 60]) {
        if (seconds % unit === 0 && seconds >= unit) return unit;
    }
    return 60;
}
function stepDelayValue(step) {
    return Math.max(1, Math.round((step.seconds || 0) / stepDelayUnit(step)));
}
function setStepDelay(step, value) {
    step.seconds = Math.max(1, value || 1) * stepDelayUnit(step);
}
function setStepUnit(step, unitSeconds) {
    step.seconds = stepDelayValue(step) * unitSeconds;
}

function iconBtn(icon, onclick) {
    return t.button(
        { type: "button", className: "btn sm circle transparent secondary", onclick },
        t.i({ className: icon }),
    );
}
function cell(control) {
    return t.div({ className: "col-lg-12" }, control);
}
function field(label, control) {
    return t.div({ className: "field" }, t.label(null, label), control);
}
function blankAutomation() {
    return {
        id: "",
        name: "",
        enabled: false,
        triggerCollection: "",
        triggerEvent: "create",
        triggerCondition: "",
        exitCondition: "",
        steps: [],
    };
}
