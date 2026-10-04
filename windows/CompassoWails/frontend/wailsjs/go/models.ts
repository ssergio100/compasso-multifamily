export namespace main {
	
	export class AddTimeResult {
	    ok: boolean;
	    message: string;
	    errorCode: string;
	    bonusSeconds: number;
	    totalSeconds: number;
	    eventUuid: string;
	
	    static createFrom(source: any = {}) {
	        return new AddTimeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.message = source["message"];
	        this.errorCode = source["errorCode"];
	        this.bonusSeconds = source["bonusSeconds"];
	        this.totalSeconds = source["totalSeconds"];
	        this.eventUuid = source["eventUuid"];
	    }
	}
	export class AgentSettings {
	    configured: boolean;
	    serverUrl: string;
	    deviceId: string;
	    controlledUserSid: string;
	    hasDeviceToken: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AgentSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.configured = source["configured"];
	        this.serverUrl = source["serverUrl"];
	        this.deviceId = source["deviceId"];
	        this.controlledUserSid = source["controlledUserSid"];
	        this.hasDeviceToken = source["hasDeviceToken"];
	    }
	}
	export class ConfigureResult {
	    ok: boolean;
	    errorCode: string;
	    message: string;
	    settings?: AgentSettings;
	
	    static createFrom(source: any = {}) {
	        return new ConfigureResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.errorCode = source["errorCode"];
	        this.message = source["message"];
	        this.settings = this.convertValues(source["settings"], AgentSettings);
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
	export class SyncState {
	    available: boolean;
	    online: boolean;
	    status: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new SyncState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.online = source["online"];
	        this.status = source["status"];
	        this.detail = source["detail"];
	    }
	}
	export class WindowsAccount {
	    name: string;
	    sid: string;
	    current: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WindowsAccount(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.sid = source["sid"];
	        this.current = source["current"];
	    }
	}

}

