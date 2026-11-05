export namespace config {
	
	export class SSH {
	    host: string;
	    port: number;
	    user: string;
	    key: string;
	
	    static createFrom(source: any = {}) {
	        return new SSH(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.port = source["port"];
	        this.user = source["user"];
	        this.key = source["key"];
	    }
	}
	export class Connection {
	    name: string;
	    driver: string;
	    host: string;
	    port: number;
	    user: string;
	    database: string;
	    file: string;
	    laravel: string;
	    color: string;
	    sslMode: string;
	    sslCA: string;
	    sslCert: string;
	    sslKey: string;
	    ssh?: SSH;
	    allowAgentWrites: boolean;
	    hideFromAgents: boolean;
	    source: string;
	    label: string;
	    missing: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Connection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.driver = source["driver"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.user = source["user"];
	        this.database = source["database"];
	        this.file = source["file"];
	        this.laravel = source["laravel"];
	        this.color = source["color"];
	        this.sslMode = source["sslMode"];
	        this.sslCA = source["sslCA"];
	        this.sslCert = source["sslCert"];
	        this.sslKey = source["sslKey"];
	        this.ssh = this.convertValues(source["ssh"], SSH);
	        this.allowAgentWrites = source["allowAgentWrites"];
	        this.hideFromAgents = source["hideFromAgents"];
	        this.source = source["source"];
	        this.label = source["label"];
	        this.missing = source["missing"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class Settings {
	    theme: string;
	    font: string;
	    omarchyAsked: boolean;
	    lerd: boolean;
	    lerdAutoConnect: boolean;
	    firstRunDone: boolean;
	    queryTimeout: number;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.theme = source["theme"];
	        this.font = source["font"];
	        this.omarchyAsked = source["omarchyAsked"];
	        this.lerd = source["lerd"];
	        this.lerdAutoConnect = source["lerdAutoConnect"];
	        this.firstRunDone = source["firstRunDone"];
	        this.queryTimeout = source["queryTimeout"];
	    }
	}

}

export namespace db {
	
	export class CloneOptions {
	    dropExisting: boolean;
	    withData: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CloneOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dropExisting = source["dropExisting"];
	        this.withData = source["withData"];
	    }
	}
	export class Column {
	    name: string;
	    type: string;
	    nullable: boolean;
	    default?: string;
	    key: string;
	    autoIncrement: boolean;
	    comment: string;
	
	    static createFrom(source: any = {}) {
	        return new Column(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.nullable = source["nullable"];
	        this.default = source["default"];
	        this.key = source["key"];
	        this.autoIncrement = source["autoIncrement"];
	        this.comment = source["comment"];
	    }
	}
	export class ForeignKey {
	    name: string;
	    columns: string[];
	    refSchema: string;
	    refTable: string;
	    refColumns: string[];
	    onUpdate: string;
	    onDelete: string;
	
	    static createFrom(source: any = {}) {
	        return new ForeignKey(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.columns = source["columns"];
	        this.refSchema = source["refSchema"];
	        this.refTable = source["refTable"];
	        this.refColumns = source["refColumns"];
	        this.onUpdate = source["onUpdate"];
	        this.onDelete = source["onDelete"];
	    }
	}
	export class Index {
	    name: string;
	    columns: string[];
	    unique: boolean;
	    primary: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Index(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.columns = source["columns"];
	        this.unique = source["unique"];
	        this.primary = source["primary"];
	    }
	}
	export class InfoItem {
	    label: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new InfoItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.value = source["value"];
	    }
	}
	export class Result {
	    sql: string;
	    columns: string[];
	    types: string[];
	    rows: any[][];
	    isQuery: boolean;
	    affected: number;
	    durationMs: number;
	    truncated: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sql = source["sql"];
	        this.columns = source["columns"];
	        this.types = source["types"];
	        this.rows = source["rows"];
	        this.isQuery = source["isQuery"];
	        this.affected = source["affected"];
	        this.durationMs = source["durationMs"];
	        this.truncated = source["truncated"];
	        this.error = source["error"];
	    }
	}
	export class Routine {
	    schema: string;
	    name: string;
	    type: string;
	    table?: string;
	
	    static createFrom(source: any = {}) {
	        return new Routine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema = source["schema"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.table = source["table"];
	    }
	}
	export class RowChange {
	    op: string;
	    where: Record<string, any>;
	    set: Record<string, any>;
	    order: string[];
	
	    static createFrom(source: any = {}) {
	        return new RowChange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.op = source["op"];
	        this.where = source["where"];
	        this.set = source["set"];
	        this.order = source["order"];
	    }
	}
	export class TableDef {
	    schema: string;
	    name: string;
	    isView: boolean;
	    columns: Column[];
	    indexes: Index[];
	    foreignKeys: ForeignKey[];
	    create: string;
	
	    static createFrom(source: any = {}) {
	        return new TableDef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema = source["schema"];
	        this.name = source["name"];
	        this.isView = source["isView"];
	        this.columns = this.convertValues(source["columns"], Column);
	        this.indexes = this.convertValues(source["indexes"], Index);
	        this.foreignKeys = this.convertValues(source["foreignKeys"], ForeignKey);
	        this.create = source["create"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SchemaOp {
	    kind: string;
	    schema: string;
	    table: string;
	    name: string;
	    column?: Column;
	    after?: string;
	    def?: TableDef;
	    index?: Index;
	    fk?: ForeignKey;
	
	    static createFrom(source: any = {}) {
	        return new SchemaOp(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.schema = source["schema"];
	        this.table = source["table"];
	        this.name = source["name"];
	        this.column = this.convertValues(source["column"], Column);
	        this.after = source["after"];
	        this.def = this.convertValues(source["def"], TableDef);
	        this.index = this.convertValues(source["index"], Index);
	        this.fk = this.convertValues(source["fk"], ForeignKey);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SortKey {
	    col: string;
	    desc: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SortKey(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.col = source["col"];
	        this.desc = source["desc"];
	    }
	}
	
	export class TableInfo {
	    schema: string;
	    name: string;
	    type: string;
	    rows?: number;
	    size?: number;
	
	    static createFrom(source: any = {}) {
	        return new TableInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema = source["schema"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.rows = source["rows"];
	        this.size = source["size"];
	    }
	}

}

export namespace history {
	
	export class Entry {
	    // Go type: time
	    time: any;
	    database: string;
	    sql: string;
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = this.convertValues(source["time"], null);
	        this.database = source["database"];
	        this.sql = source["sql"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Snippet {
	    name: string;
	    sql: string;
	
	    static createFrom(source: any = {}) {
	        return new Snippet(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.sql = source["sql"];
	    }
	}

}

export namespace main {
	
	export class Bootstrap {
	    version: string;
	    settings: config.Settings;
	    theme: theme.Theme;
	    themes: theme.Info[];
	    omarchy: boolean;
	    askOmarchy: boolean;
	    configDir: string;
	    executable: string;
	
	    static createFrom(source: any = {}) {
	        return new Bootstrap(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.settings = this.convertValues(source["settings"], config.Settings);
	        this.theme = this.convertValues(source["theme"], theme.Theme);
	        this.themes = this.convertValues(source["themes"], theme.Info);
	        this.omarchy = source["omarchy"];
	        this.askOmarchy = source["askOmarchy"];
	        this.configDir = source["configDir"];
	        this.executable = source["executable"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class BrowseResult {
	    result: db.Result;
	    total: number;
	
	    static createFrom(source: any = {}) {
	        return new BrowseResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.result = this.convertValues(source["result"], db.Result);
	        this.total = source["total"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CSVFile {
	    path: string;
	    columns: string[];
	    rows: string[][];
	
	    static createFrom(source: any = {}) {
	        return new CSVFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.columns = source["columns"];
	        this.rows = source["rows"];
	    }
	}
	export class ConnectionList {
	    connections: config.Connection[];
	    warnings: string[];
	    open: Record<string, boolean>;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connections = this.convertValues(source["connections"], config.Connection);
	        this.warnings = source["warnings"];
	        this.open = source["open"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class QueryRun {
	    results: db.Result[];
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new QueryRun(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.results = this.convertValues(source["results"], db.Result);
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TreeDatabase {
	    tables: db.TableInfo[];
	    routines: db.Routine[];
	
	    static createFrom(source: any = {}) {
	        return new TreeDatabase(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tables = this.convertValues(source["tables"], db.TableInfo);
	        this.routines = this.convertValues(source["routines"], db.Routine);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace theme {
	
	export class Info {
	    id: string;
	    name: string;
	    source: string;
	    mode: string;
	
	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.source = source["source"];
	        this.mode = source["mode"];
	    }
	}
	export class Theme {
	    id: string;
	    name: string;
	    source: string;
	    colors: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new Theme(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.source = source["source"];
	        this.colors = source["colors"];
	    }
	}

}

