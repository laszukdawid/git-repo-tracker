import Clutter from 'gi://Clutter';
import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import GObject from 'gi://GObject';
import St from 'gi://St';

import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PanelMenu from 'resource:///org/gnome/shell/ui/panelMenu.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';

const REFRESH_SECONDS = 30;
const MAX_REPOS_PER_GROUP = 18;

function appCommand(args = []) {
    const configured = GLib.getenv('GIT_REPO_TRACKER_APP');
    let exe = configured && configured.trim() !== '' ? configured : 'git-repo-tracker';
    if (exe === 'git-repo-tracker') {
        const local = GLib.build_filenamev([GLib.get_home_dir(), '.local', 'bin', 'git-repo-tracker']);
        if (GLib.file_test(local, GLib.FileTest.IS_EXECUTABLE))
            exe = local;
    }
    return [exe, ...args];
}

function cliExe() {
    const configured = GLib.getenv('GIT_REPO_TRACKER_CLI');
    let exe = configured && configured.trim() !== '' ? configured : 'git-repo-tracker-cli';
    if (exe === 'git-repo-tracker-cli') {
        const local = GLib.build_filenamev([GLib.get_home_dir(), '.local', 'bin', 'git-repo-tracker-cli']);
        if (GLib.file_test(local, GLib.FileTest.IS_EXECUTABLE))
            exe = local;
    }
    return exe;
}

function cliCommand(refresh = false) {
    const argv = [cliExe(), 'status', '--json'];
    if (refresh)
        argv.push('--refresh');
    return argv;
}

function detailCommand(path) {
    return [cliExe(), 'details', path];
}

function openCommand(path) {
    return [cliExe(), 'open', path];
}

function collapseHome(path) {
    const home = GLib.get_home_dir();
    if (path === home)
        return '~';
    if (path.startsWith(home + '/'))
        return '~' + path.slice(home.length);
    return path;
}

function dirname(path) {
    const i = path.lastIndexOf('/');
    if (i <= 0)
        return path;
    return path.slice(0, i);
}

function repoGlyph(repo) {
    if (repo.err || repo.fetchErr)
        return '!';
    if (repo.dirty)
        return '!';
    if (repo.behind > 0)
        return '↓';
    return '✓';
}

function repoGlyphClass(repo) {
    if (repo.err || repo.fetchErr)
        return 'grt-glyph grt-glyph-error';
    if (repo.dirty)
        return 'grt-glyph grt-glyph-dirty';
    if (repo.behind > 0)
        return 'grt-glyph grt-glyph-behind';
    return 'grt-glyph grt-glyph-clean';
}

function groupStatus(group) {
    const parts = [];
    if (group.errors > 0)
        parts.push(`${group.errors} error${group.errors === 1 ? '' : 's'}`);
    if (group.behind > 0)
        parts.push(`${group.behind} behind`);
    if (group.dirty > 0)
        parts.push(`${group.dirty} dirty`);
    return parts.length > 0 ? parts.join(' · ') : 'up to date';
}

function groupStatusClass(group) {
    if (group.errors > 0)
        return 'grt-error';
    if (group.behind > 0 || group.dirty > 0)
        return 'grt-section-status';
    return 'grt-muted';
}

function roundButton(iconName, onClick) {
    const button = new St.Button({style_class: 'grt-footer-button', can_focus: true});
    button.child = new St.Icon({icon_name: iconName, style_class: 'grt-footer-icon'});
    button.connect('clicked', onClick);
    return button;
}

function spawnDetached(argv) {
    try {
        Gio.Subprocess.new(argv, Gio.SubprocessFlags.NONE);
    } catch (e) {
        logError(e, `git-repo-tracker: failed to launch ${argv.join(' ')}`);
    }
}

function spawnLogged(argv) {
    let proc;
    try {
        proc = Gio.Subprocess.new(
            argv,
            Gio.SubprocessFlags.STDOUT_PIPE | Gio.SubprocessFlags.STDERR_PIPE
        );
    } catch (e) {
        logError(e, `git-repo-tracker: failed to launch ${argv.join(' ')}`);
        return;
    }
    proc.communicate_utf8_async(null, null, (_proc, res) => {
        try {
            const [, stdout, stderr] = proc.communicate_utf8_finish(res);
            if (!proc.get_successful())
                log(`git-repo-tracker: ${argv.join(' ')} failed: ${stderr || stdout || 'no output'}`);
        } catch (e) {
            logError(e, `git-repo-tracker: failed waiting for ${argv.join(' ')}`);
        }
    });
}

function spawnLoggedWithDone(argv, onDone) {
    let proc;
    try {
        proc = Gio.Subprocess.new(
            argv,
            Gio.SubprocessFlags.STDOUT_PIPE | Gio.SubprocessFlags.STDERR_PIPE
        );
    } catch (e) {
        logError(e, `git-repo-tracker: failed to launch ${argv.join(' ')}`);
        onDone(false, '', e.message);
        return;
    }
    proc.communicate_utf8_async(null, null, (_proc, res) => {
        try {
            const [, stdout, stderr] = proc.communicate_utf8_finish(res);
            const ok = proc.get_successful();
            if (!ok)
                log(`git-repo-tracker: ${argv.join(' ')} failed: ${stderr || stdout || 'no output'}`);
            onDone(ok, stdout, stderr);
        } catch (e) {
            logError(e, `git-repo-tracker: failed waiting for ${argv.join(' ')}`);
            onDone(false, '', e.message);
        }
    });
}

function openRichMode() {
    log('git-repo-tracker: opening rich mode');
    spawnLogged(appCommand([]));
}

function humanizeTime(value) {
    if (!value)
        return 'unknown';
    const t = new Date(value);
    if (Number.isNaN(t.getTime()) || t.getUTCFullYear() <= 1)
        return 'unknown';
    const d = Date.now() - t.getTime();
    if (d < 60 * 1000)
        return 'just now';
    if (d < 60 * 60 * 1000)
        return `${Math.floor(d / (60 * 1000))}m ago`;
    if (d < 24 * 60 * 60 * 1000)
        return `${Math.floor(d / (60 * 60 * 1000))}h ago`;
    if (d < 7 * 24 * 60 * 60 * 1000)
        return `${Math.floor(d / (24 * 60 * 60 * 1000))}d ago`;
    return t.toISOString().slice(0, 10);
}

function commitMeta(hash, time) {
    return `${hash} · ${humanizeTime(time)}`;
}

function repoErrorMessage(repo) {
    if (repo.fetchErr)
        return `Fetch error: ${repo.fetchErr}`;
    if (repo.err)
        return `Status error: ${repo.err}`;
    return '';
}

function detailLine(text, styleClass) {
    return new St.Label({
        text,
        style_class: styleClass,
        x_expand: true,
        x_align: Clutter.ActorAlign.START,
    });
}

const RepoRow = GObject.registerClass(
class RepoRow extends PopupMenu.PopupBaseMenuItem {
    _init(repo, options) {
        super._init({reactive: true});
        this._repo = repo;
        this._options = options;
        this._suppressActivate = false;
        this.actor.add_style_class_name('grt-repo-row');

        const root = new St.BoxLayout({vertical: true, x_expand: true, style_class: 'grt-row-root'});
        const summary = new St.BoxLayout({vertical: false, x_expand: true, style_class: 'grt-row-summary'});

        summary.add_child(new St.Label({
            text: repoGlyph(repo),
            style_class: repoGlyphClass(repo),
            y_align: Clutter.ActorAlign.CENTER,
        }));

        const labels = new St.BoxLayout({vertical: false, x_expand: true});
        this._name = new St.Label({text: repo.name || dirname(repo.path), style_class: 'grt-row-name'});
        labels.add_child(this._name);

        const branch = repo.branch && repo.branch.trim() !== '' ? repo.branch : '-';
        this._branch = new St.Label({text: `  ${branch}`, style_class: 'grt-row-branch'});
        labels.add_child(this._branch);
        summary.add_child(labels);

        const value = [];
        if (repo.behind > 0)
            value.push(`↓${repo.behind}`);
        if (repo.ahead > 0)
            value.push(`↑${repo.ahead}`);
        if (repo.dirty)
            value.push('dirty');
        if (repo.linesAdded > 0 || repo.linesDeleted > 0)
            value.push(`+${repo.linesAdded}/-${repo.linesDeleted}`);
        if (repo.err || repo.fetchErr)
            value.push('!');

        this._value = new St.Label({
            text: value.join('  '),
            style_class: repo.err || repo.fetchErr ? 'grt-row-value grt-error' : 'grt-row-value',
            x_align: Clutter.ActorAlign.END,
            x_expand: true,
        });
        summary.add_child(this._value);

        this._toggle = new St.Button({
            style_class: 'grt-row-pull-button',
            can_focus: true,
            reactive: true,
        });
        this._toggle.child = new St.Icon({
            icon_name: options.expanded ? 'pan-down-symbolic' : 'pan-end-symbolic',
            style_class: 'grt-row-pull-icon',
        });
        this._toggle.connect('clicked', () => {
            this._suppressActivate = true;
            options.onToggle(repo.path);
        });
        summary.add_child(this._toggle);

        if (repo.behind > 0 && !repo.err && !repo.fetchErr) {
            this._pull = new St.Button({
                style_class: 'grt-row-pull-button',
                can_focus: true,
                reactive: true,
            });
            this._pull.child = new St.Icon({
                icon_name: 'folder-download-symbolic',
                style_class: 'grt-row-pull-icon',
            });
            this._pull.connect('clicked', () => {
                this._suppressActivate = true;
                this._pull.reactive = false;
                this._pull.can_focus = false;
                options.onPull(repo.path);
            });
            summary.add_child(this._pull);
        }

        root.add_child(summary);

        if (options.expanded) {
            root.add_child(this._buildDetails(options.detailState));
        }

        this.add_child(root);
    }

    activate(event) {
        if (this._suppressActivate) {
            this._suppressActivate = false;
            return;
        }
        if (this._repo.path)
            this._options.onOpen(this._repo.path);
    }

    _buildDetails(detailState) {
        const box = new St.BoxLayout({vertical: true, x_expand: true, style_class: 'grt-row-detail-box'});
        const err = repoErrorMessage(this._repo);
        if (err)
            box.add_child(detailLine(err, 'grt-row-detail-line grt-row-detail-error'));
        if (!detailState || detailState.loading) {
            if (!err)
                box.add_child(detailLine('Loading…', 'grt-row-detail-line grt-row-detail-muted'));
            return box;
        }
        if (detailState.error) {
            box.add_child(detailLine(detailState.error, 'grt-row-detail-line grt-row-detail-error'));
            return box;
        }
        const detail = detailState.detail;
        if (!detail)
            return box;
        let head = detail.path || this._repo.path;
        if (this._repo.behind > 0)
            head += ` · behind ${this._repo.behind} commits`;
        box.add_child(detailLine(head, 'grt-row-detail-line grt-row-detail-muted'));

        const branch = this._repo.branch && this._repo.branch.trim() !== '' ? this._repo.branch : '-';
        box.add_child(detailLine(`Local ${branch}`, 'grt-row-detail-line grt-row-detail-key'));
        if (detail.localHash) {
            box.add_child(detailLine(commitMeta(detail.localHash, detail.localTime), 'grt-row-detail-line grt-row-detail-meta'));
            if (detail.localMsg)
                box.add_child(detailLine(detail.localMsg, 'grt-row-detail-line grt-row-detail-muted'));
        } else {
            box.add_child(detailLine('—', 'grt-row-detail-line grt-row-detail-meta'));
        }

        box.add_child(detailLine(`Origin ${detail.originRef || '—'}`, 'grt-row-detail-line grt-row-detail-key'));
        if (detail.originHash) {
            box.add_child(detailLine(commitMeta(detail.originHash, detail.originTime), 'grt-row-detail-line grt-row-detail-meta'));
            if (detail.originMsg)
                box.add_child(detailLine(detail.originMsg, 'grt-row-detail-line grt-row-detail-muted'));
        } else {
            box.add_child(detailLine('—', 'grt-row-detail-line grt-row-detail-meta'));
        }
        return box;
    }
});

const Indicator = GObject.registerClass(
class Indicator extends PanelMenu.Button {
    _init(extension) {
        super._init(0.0, 'Git Repo Tracker');
        this._extension = extension;
        this._timeoutId = 0;
        this._lastDoc = null;
        this._query = '';
        this._expandedPath = '';
        this._details = new Map();

        const box = new St.BoxLayout({style_class: 'grt-panel-box'});
        box.add_child(new St.Label({
            text: '⎇',
            style_class: 'grt-panel-glyph',
            y_align: Clutter.ActorAlign.CENTER,
        }));
        this._panelLabel = new St.Label({text: '...', y_align: Clutter.ActorAlign.CENTER, style_class: 'grt-panel-label'});
        box.add_child(this._panelLabel);
        this.add_child(box);
        this.menu.box.add_style_class_name('grt-menu');

        this.menu.connect('open-state-changed', (_menu, open) => {
            if (open)
                this._refresh(true);
        });

        this._buildLoadingMenu();
        this._refresh(false);
        this._timeoutId = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, REFRESH_SECONDS, () => {
            this._refresh(false);
            return GLib.SOURCE_CONTINUE;
        });
    }

    destroy() {
        if (this._timeoutId) {
            GLib.Source.remove(this._timeoutId);
            this._timeoutId = 0;
        }
        super.destroy();
    }

    _buildLoadingMenu() {
        this.menu.removeAll();
        const item = new PopupMenu.PopupMenuItem('Loading repositories...');
        item.setSensitive(false);
        this.menu.addMenuItem(item);
    }

    _refresh(refresh) {
        let proc;
        try {
            proc = Gio.Subprocess.new(
                cliCommand(refresh),
                Gio.SubprocessFlags.STDOUT_PIPE | Gio.SubprocessFlags.STDERR_PIPE
            );
        } catch (e) {
            this._renderError(e.message);
            return;
        }
        proc.communicate_utf8_async(null, null, (_proc, res) => {
            try {
                const [, stdout, stderr] = proc.communicate_utf8_finish(res);
                if (proc.get_successful()) {
                    this._lastDoc = JSON.parse(stdout);
                    this._render(this._lastDoc);
                } else {
                    this._renderError(stderr || 'CLI failed');
                }
            } catch (e) {
                this._renderError(e.message);
            }
        });
    }

    _renderError(message) {
        this._panelLabel.text = '!';
        this.menu.removeAll();
        const err = new PopupMenu.PopupMenuItem(`git-repo-tracker error: ${message}`);
        err.setSensitive(false);
        this.menu.addMenuItem(err);
        this._addActions();
    }

    _render(doc, keepSearchFocus = false) {
        const total = doc.total || 0;
        const behind = doc.behind || 0;
        this._panelLabel.text = behind > 0 ? `${behind}/${total}` : `${total}`;

        this.menu.removeAll();
        const header = new PopupMenu.PopupBaseMenuItem({reactive: false, style_class: 'grt-header'});
        header.add_child(new St.Label({text: `${total} repos · ${behind} behind`, style_class: 'grt-header-label'}));
        this.menu.addMenuItem(header);

        const searchItem = new PopupMenu.PopupBaseMenuItem({reactive: false, style_class: 'grt-search-row'});
        const search = new St.Entry({
            hint_text: 'Search repositories...',
            text: this._query,
            can_focus: true,
            x_expand: true,
            style_class: 'grt-search-entry',
        });
        search.clutter_text.connect('text-changed', () => {
            this._query = search.get_text().toLowerCase();
            if (this._lastDoc)
                this._render(this._lastDoc, true);
        });
        searchItem.add_child(search);
        this.menu.addMenuItem(searchItem);
        if (keepSearchFocus) {
            GLib.idle_add(GLib.PRIORITY_DEFAULT_IDLE, () => {
                global.stage.set_key_focus(search.clutter_text);
                return GLib.SOURCE_REMOVE;
            });
        }

        this.menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

        const groups = this._groupRepos(doc.repos || []);
        const visiblePaths = new Set();
        if (groups.length === 0) {
            const empty = new PopupMenu.PopupMenuItem(this._query ? 'No matching repositories' : 'No repositories found');
            empty.setSensitive(false);
            this.menu.addMenuItem(empty);
        }
        for (const group of groups) {
            const title = `${group.name}   ${group.repos.length} repos`;
            const section = new PopupMenu.PopupSubMenuMenuItem(title, true);
            section.actor.add_style_class_name('grt-group-header');
            const status = new St.Label({
                text: groupStatus(group),
                style_class: groupStatusClass(group),
                x_align: Clutter.ActorAlign.END,
                x_expand: true,
            });
            section.actor.insert_child_at_index(status, 4);
            this.menu.addMenuItem(section);

            const shown = group.repos.slice(0, MAX_REPOS_PER_GROUP);
            for (const repo of shown) {
                visiblePaths.add(repo.path);
                const expanded = repo.path === this._expandedPath;
                if (expanded)
                    this._ensureDetails(repo.path);
                section.menu.addMenuItem(new RepoRow(repo, {
                    expanded,
                    detailState: this._details.get(repo.path) || null,
                    onOpen: path => this._openRepo(path),
                    onPull: path => this._pullRepo(path),
                    onToggle: path => this._toggleExpand(path),
                }));
            }
            if (group.repos.length > shown.length) {
                const rest = new PopupMenu.PopupMenuItem(`...and ${group.repos.length - shown.length} more`);
                rest.setSensitive(false);
                section.menu.addMenuItem(rest);
            }
        }
        if (this._expandedPath && !visiblePaths.has(this._expandedPath))
            this._expandedPath = '';
        this._addActions();
    }

    _groupRepos(repos) {
        const groups = new Map();
        for (const repo of repos) {
            if (this._query) {
                const haystack = `${repo.name || ''} ${repo.branch || ''} ${repo.path || ''}`.toLowerCase();
                if (!haystack.includes(this._query))
                    continue;
            }
            const group = collapseHome(dirname(repo.path || ''));
            if (!groups.has(group))
                groups.set(group, {name: group, repos: [], behind: 0, dirty: 0, errors: 0});
            const g = groups.get(group);
            g.repos.push(repo);
            if (repo.err || repo.fetchErr)
                g.errors++;
            if (repo.behind > 0)
                g.behind++;
            if (repo.dirty)
                g.dirty++;
        }
        const out = Array.from(groups.values());
        for (const group of out) {
            group.repos.sort((a, b) => {
                const ae = a.err || a.fetchErr ? 1 : 0;
                const be = b.err || b.fetchErr ? 1 : 0;
                if (ae !== be)
                    return be - ae;
                if ((a.behind || 0) !== (b.behind || 0))
                    return (b.behind || 0) - (a.behind || 0);
                return (a.name || '').localeCompare(b.name || '');
            });
        }
        out.sort((a, b) => a.name.localeCompare(b.name));
        return out;
    }

    _pullRepo(path) {
        this._details.delete(path);
        spawnLoggedWithDone([cliExe(), 'pull', path], () => {
            this._refresh(true);
        });
    }

    _openRepo(path) {
        spawnLogged(openCommand(path));
    }

    _toggleExpand(path) {
        if (this._expandedPath === path) {
            this._expandedPath = '';
            if (this._lastDoc)
                this._render(this._lastDoc);
            return;
        }
        this._expandedPath = path;
        this._ensureDetails(path);
        if (this._lastDoc)
            this._render(this._lastDoc);
    }

    _ensureDetails(path) {
        const current = this._details.get(path);
        if (current && (current.loading || current.detail || current.error))
            return;
        this._details.set(path, {loading: true, detail: null, error: ''});
        spawnLoggedWithDone(detailCommand(path), (ok, stdout, stderr) => {
            let next = {loading: false, detail: null, error: stderr || 'Failed to load details'};
            if (ok) {
                try {
                    next = {loading: false, detail: JSON.parse(stdout), error: ''};
                } catch (e) {
                    next = {loading: false, detail: null, error: e.message};
                }
            }
            this._details.set(path, next);
            if (this._expandedPath === path && this._lastDoc)
                this._render(this._lastDoc);
        });
    }

    _addActions() {
        this.menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());
        const item = new PopupMenu.PopupBaseMenuItem({reactive: false, style_class: 'grt-footer'});
        const box = new St.BoxLayout({style_class: 'grt-footer-box', x_expand: true});
        box.add_child(roundButton('view-refresh-symbolic', () => this._refresh(true)));
        box.add_child(roundButton('window-new-symbolic', () => {
            openRichMode();
        }));
        box.add_child(roundButton('folder-download-symbolic', () => {
            spawnDetached([cliExe(), 'update-all']);
        }));
        box.add_child(roundButton('preferences-system-symbolic', () => {
            log('git-repo-tracker: opening settings');
            spawnLogged(appCommand(['--settings']));
        }));
        item.add_child(box);
        this.menu.addMenuItem(item);
    }
});

export default class GitRepoTrackerExtension extends Extension {
    enable() {
        this._indicator = new Indicator(this);
        Main.panel.addToStatusArea(this.uuid, this._indicator);
    }

    disable() {
        if (this._indicator) {
            this._indicator.destroy();
            this._indicator = null;
        }
    }
}
