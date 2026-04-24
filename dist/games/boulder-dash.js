// boulder-dash.js — Boulder Dash for dev-null
// Load with: /game load boulder-dash

// ============================================================
// Tile constants
// ============================================================
var EMPTY=0, DIRT=1, WALL=2, BOULDER=3, DIAMOND=4,
    EXIT_C=5, EXIT_O=6, AMOEBA=7, MAGIC_WALL=8;

// Directions
var UP=0, DOWN=1, LEFT=2, RIGHT=3;
var DX=[0,0,-1,1], DY=[-1,1,0,0];
// TURN_L[d] = direction 90° to the left of d
// UP→LEFT, DOWN→RIGHT, LEFT→DOWN, RIGHT→UP
var TURN_L=[2,3,1,0];
// TURN_R[d] = direction 90° to the right of d
// UP→RIGHT, DOWN→LEFT, LEFT→UP, RIGHT→DOWN
var TURN_R=[3,2,0,1];
// OPP[d] = opposite of d
var OPP=[1,0,3,2];

// ============================================================
// Timing & scoring constants
// ============================================================
var PHYSICS_INTERVAL = 0.15;
var ENEMY_INTERVAL   = 0.30;
var AMOEBA_INTERVAL  = 0.40;
var RESPAWN_TIME     = 3.0;
var INVULN_TIME      = 2.0;
var CAVE_WIN_DELAY   = 2.5;
var AMOEBA_MAX       = 200;
var PTS_DIAMOND      = 10;
var PTS_TIME         = 5;
var PTS_CAVE         = 500;

// ============================================================
// Colors
// ============================================================
var C_DIRT_FG="#AA5500", C_DIRT_BG="#331100";
var C_WALL_FG="#888888", C_WALL_BG="#444444";
var C_BOULDER="#AAAAAA";
var C_DIA_A="#00FFFF", C_DIA_B="#005588";
var C_FIREFLY="#FF4400";
var C_BUTTERFLY="#FF44FF";
var C_AMOEBA="#00AA00";
var C_MAGIC_FG="#AA00AA", C_MAGIC_BG="#220022";
var C_EXIT_C="#555555";
var C_EXIT_O="#00FF00";
var C_PLAYER="#FFFF00";
var C_OTHER="#FF8800";
var C_DEAD_FG="#444444";
var C_EXPLO="#FF8800";

// ============================================================
// Cave definitions
// Cave chars: #=wall .=dirt ' '=empty O=boulder *=diamond
//             P=player-start X=exit Q=firefly B=butterfly
//             A=amoeba M=magic-wall
// Each row is padded to the max-row-length with '#' by parseGrid.
// ============================================================
var CAVES = [
  // ── Cave A ─────────────────────────────────────────────────
  {
    name: "A", title: "Rookie Mine",
    diamondsNeeded: 6, timeLimit: 150, magicWallDur: 0,
    raw: [
      "########################################",
      "#......................................P#",
      "#....*.......O.........................#",
      "#......................................#",
      "#.O....*.............O.................#",
      "#......................................#",
      "#...............*......................#",
      "#......................................#",
      "#....*.O.......O.......................#",
      "#......................................#",
      "#.......*..............................#",
      "#......................................#",
      "#.O.............O......*...............#",
      "#......................................#",
      "#.......*..............................#",
      "#......................................#",
      "#.X....................................#",
      "########################################"
    ]
  },
  // ── Cave B ─────────────────────────────────────────────────
  {
    name: "B", title: "Rolling Stones",
    diamondsNeeded: 10, timeLimit: 120, magicWallDur: 0,
    raw: [
      "########################################",
      "#P..*..OOO.......OO...*................#",
      "#......................................#",
      "#..*.....O.O...*......OOOO.............#",
      "#......................................#",
      "#.OOOO.........*.....O.O.....*........#",
      "#......................................#",
      "##########                  ###########",
      "#Q        .....*............Q         #",
      "#         .....*............          #",
      "##########                  ###########",
      "#.*....OO..........OO.*.................#",
      "#......................................#",
      "#.......*....OOOO....*........OO......#",
      "#......................................#",
      "#.OO.....*............................#",
      "#.....................................X#",
      "########################################"
    ]
  },
  // ── Cave C ─────────────────────────────────────────────────
  // Butterflies turn into 3×3 diamonds when crushed.
  // Dig under the boulders above their rooms to release them.
  {
    name: "C", title: "Butterfly Garden",
    diamondsNeeded: 12, timeLimit: 120, magicWallDur: 0,
    raw: [
      "########################################",
      "#P......................................#",
      "#.......OOO.....*.......OOO............#",
      "#......................................#",
      "##########.##########.##########.######",
      "#B                   B                #",
      "#         ...........                 #",
      "#         ...........                 #",
      "##########.##########.##########.######",
      "#......................................#",
      "#......OOO......*.......OOO............#",
      "#......................................#",
      "##########.##########.##########.######",
      "#B                   B                #",
      "#         ...........                 #",
      "#         ...........                 #",
      "##########.##########.##########.######",
      "#......*.....*......*.....*..........X#",
      "########################################"
    ]
  },
  // ── Cave D ─────────────────────────────────────────────────
  {
    name: "D", title: "Firefly Alley",
    diamondsNeeded: 14, timeLimit: 100, magicWallDur: 0,
    raw: [
      "########################################",
      "#P...*......*.......*.....*............#",
      "#.###########.###########.###########.#",
      "#.#Q         #Q          #Q          .#",
      "#.#          #           #           .#",
      "#.#          #           #           .#",
      "#.#          #           #           .#",
      "#.###########.###########.###########.#",
      "#......................................#",
      "#...*......*.......*......*............#",
      "#.###########.###########.###########.#",
      "#.#Q         #Q          #           .#",
      "#.#          #           #           .#",
      "#.#          #           #           .#",
      "#.#          #           #           .#",
      "#.###########.###########.###########.#",
      "#...*......*.......*......*.............#",
      "#.....................................X#",
      "########################################"
    ]
  },
  // ── Cave E ─────────────────────────────────────────────────
  // Features: magic walls (boulders→diamonds when they pass through),
  // amoeba (grows, suffocate it for diamonds), both enemy types.
  {
    name: "E", title: "The Gauntlet",
    diamondsNeeded: 20, timeLimit: 90, magicWallDur: 25,
    raw: [
      "########################################",
      "#P....*......*......*......*..........#",
      "#......................................#",
      "#..OOOOOO...OOOOOO...OOOOOO...OOOOOO.#",
      "#......................................#",
      "#MMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMM#",
      "#......................................#",
      "#....AA......AA......AA......AA.......#",
      "#....AA......AA......AA......AA.......#",
      "#......................................#",
      "#################  ####################",
      "#Q               ##B                  #",
      "#                ##                   #",
      "#                ##                   #",
      "#################  ####################",
      "#......................................#",
      "#..*.......*.....*.......*...........#",
      "#.....................................X#",
      "########################################"
    ]
  }
];

// ============================================================
// All mutable game state lives in Game.state; _s is bound to it at the
// start of each hook so the existing helpers reference _s.X unchanged.
var _s = null;

function initialState() {
    return {
        grid:[], fallingGrid:[], movedGrid:[],
        CAVE_W:0, CAVE_H:0,
        enemies:[], amoebaList:[],
        pls:{}, plOrder:[],
        caveIndex:0, caveData:null,
        caveName:"", caveTitle:"",
        startX:1, startY:1,
        diamondsNeeded:0, diamondsCollected:0,
        timeLeft:0, exitOpen:false,
        magicWallActive:false, magicWallTimer:0,
        physicsTimer:0, enemyTimer:0, amoebaTimer:0,
        elapsed:0, wonDelay:0, gameEnded:false, highScore:0
    };
}

// ============================================================
// Helpers
// ============================================================
function clamp(v,lo,hi){ return v<lo?lo:v>hi?hi:v; }
function rng(){ return Math.random(); }

function inBounds(x,y){
    return x>=0 && x<_s.CAVE_W && y>=0 && y<_s.CAVE_H;
}

function playerAt(x,y){
    for(var id in _s.pls){
        var p=_s.pls[id];
        if(!p.dead && !p.exited && p.x===x && p.y===y) return id;
    }
    return null;
}

function enemyAt(x,y){
    for(var i=0;i<_s.enemies.length;i++){
        if(_s.enemies[i].x===x && _s.enemies[i].y===y) return i;
    }
    return -1;
}

// ============================================================
// Cave loading
// ============================================================
function parseGrid(raw){
    var maxW=0;
    for(var i=0;i<raw.length;i++){
        if(raw[i].length>maxW) maxW=raw[i].length;
    }
    _s.CAVE_H=raw.length;
    _s.CAVE_W=maxW;
    var g=[], fg=[], mg=[], ens=[], amL=[], sx=1, sy=1;
    for(var y=0;y<_s.CAVE_H;y++){
        var row=raw[y];
        while(row.length<maxW) row+="#";
        g.push([]); fg.push([]); mg.push([]);
        for(var x=0;x<maxW;x++){
            var ch=row[x], tile=EMPTY;
            switch(ch){
                case '#': tile=WALL; break;
                case '.': tile=DIRT; break;
                case 'O': tile=BOULDER; break;
                case '*': tile=DIAMOND; break;
                case 'X': tile=EXIT_C; break;
                case 'A': tile=AMOEBA; amL.push({x:x,y:y}); break;
                case 'M': tile=MAGIC_WALL; break;
                case 'P': tile=EMPTY; sx=x; sy=y; break;
                case 'Q': tile=EMPTY; ens.push({type:"firefly",x:x,y:y,dir:RIGHT}); break;
                case 'B': tile=EMPTY; ens.push({type:"butterfly",x:x,y:y,dir:LEFT}); break;
                default:  tile=EMPTY; break;  // space and unknown = empty
            }
            g[y].push(tile); fg[y].push(false); mg[y].push(false);
        }
    }
    return {g:g,fg:fg,mg:mg,ens:ens,amL:amL,sx:sx,sy:sy};
}

function loadCave(idx){
    _s.caveData          = CAVES[idx];
    _s.caveIndex         = idx;
    _s.caveName          = _s.caveData.name;
    _s.caveTitle         = _s.caveData.title;
    _s.diamondsNeeded    = _s.caveData.diamondsNeeded;
    _s.diamondsCollected = 0;
    _s.timeLeft          = _s.caveData.timeLimit;
    _s.exitOpen          = false;
    _s.magicWallActive   = (_s.caveData.magicWallDur > 0);
    _s.magicWallTimer    = _s.caveData.magicWallDur || 0;
    _s.wonDelay          = 0;
    _s.physicsTimer=0; _s.enemyTimer=0; _s.amoebaTimer=0;

    var parsed=parseGrid(_s.caveData.raw);
    _s.grid=parsed.g; _s.fallingGrid=parsed.fg; _s.movedGrid=parsed.mg;
    _s.enemies=parsed.ens; _s.amoebaList=parsed.amL;
    _s.startX=parsed.sx; _s.startY=parsed.sy;

    // Place / reset all living players
    for(var id in _s.pls){
        var p=_s.pls[id];
        if(p.lives>0){
            p.x=_s.startX; p.y=_s.startY;
            p.dead=false; p.exited=false;
            p.respawnTimer=0; p.invulnTimer=INVULN_TIME;
        }
    }
}

// ============================================================
// Physics
// ============================================================
function clearMoved(){
    for(var y=0;y<_s.CAVE_H;y++)
        for(var x=0;x<_s.CAVE_W;x++)
            _s.movedGrid[y][x]=false;
}

// Called when a heavy object lands at (x,y).
function checkCrush(x,y){
    var pid=playerAt(x,y);
    if(pid!==null) triggerDeath(pid);

    var ei=enemyAt(x,y);
    if(ei>=0){
        var yTile=(_s.enemies[ei].type==="butterfly")?DIAMOND:EMPTY;
        _s.enemies.splice(ei,1);
        doExplosion(x,y,yTile);
    }
}

function doExplosion(cx,cy,yTile){
    for(var dy=-1;dy<=1;dy++){
        for(var dx=-1;dx<=1;dx++){
            var ex=cx+dx, ey=cy+dy;
            if(!inBounds(ex,ey)) continue;
            if(_s.grid[ey][ex]===WALL) continue;
            _s.grid[ey][ex]=yTile;
            _s.fallingGrid[ey][ex]=false;
            _s.movedGrid[ey][ex]=true;
            // Kill players in blast
            var pid=playerAt(ex,ey);
            if(pid!==null) triggerDeath(pid);
            // Remove any other enemies in blast
            for(var i=_s.enemies.length-1;i>=0;i--){
                if(_s.enemies[i].x===ex && _s.enemies[i].y===ey) _s.enemies.splice(i,1);
            }
        }
    }
    // Diamonds placed by explosions appear in the grid; players collect by walking over them.
}

function physicsTick(){
    clearMoved();
    for(var y=0;y<_s.CAVE_H-1;y++){
        for(var x=0;x<_s.CAVE_W;x++){
            if(_s.movedGrid[y][x]) continue;
            var tile=_s.grid[y][x];
            if(tile!==BOULDER && tile!==DIAMOND) continue;

            var below=_s.grid[y+1][x];

            // ── Magic wall pass-through ──────────────────────
            if(below===MAGIC_WALL && _s.magicWallActive){
                if(y+2<_s.CAVE_H && _s.grid[y+2][x]===EMPTY && !_s.movedGrid[y+2][x]){
                    _s.grid[y][x]=EMPTY;
                    _s.fallingGrid[y][x]=false;
                    var out=(tile===BOULDER)?DIAMOND:BOULDER;
                    _s.grid[y+2][x]=out;
                    _s.fallingGrid[y+2][x]=true;
                    _s.movedGrid[y+2][x]=true;
                    checkCrush(x,y+2);
                }
                continue;
            }

            // ── Direct fall ──────────────────────────────────
            // Note: no player/enemy exclusion — falling onto them is the crush mechanic.
            if(below===EMPTY && !_s.movedGrid[y+1][x]){
                _s.grid[y][x]=EMPTY;
                _s.fallingGrid[y][x]=false;
                _s.grid[y+1][x]=tile;
                _s.fallingGrid[y+1][x]=true;
                _s.movedGrid[y+1][x]=true;
                checkCrush(x,y+1);
                continue;
            }

            // ── Roll off round surfaces ──────────────────────
            // Only rolls if was actively falling last tick (fallingGrid).
            if(_s.fallingGrid[y][x] && (below===BOULDER||below===DIAMOND||below===WALL||below===MAGIC_WALL)){
                // Try left
                if(x>0 && _s.grid[y][x-1]===EMPTY && _s.grid[y+1][x-1]===EMPTY && !_s.movedGrid[y][x-1]){
                    _s.grid[y][x]=EMPTY;
                    _s.fallingGrid[y][x]=false;
                    _s.grid[y][x-1]=tile;
                    _s.fallingGrid[y][x-1]=true;
                    _s.movedGrid[y][x-1]=true;
                    continue;
                }
                // Try right
                if(x+1<_s.CAVE_W && _s.grid[y][x+1]===EMPTY && _s.grid[y+1][x+1]===EMPTY && !_s.movedGrid[y][x+1]){
                    _s.grid[y][x]=EMPTY;
                    _s.fallingGrid[y][x]=false;
                    _s.grid[y][x+1]=tile;
                    _s.fallingGrid[y][x+1]=true;
                    _s.movedGrid[y][x+1]=true;
                    continue;
                }
            }

            // Settled this tick
            _s.fallingGrid[y][x]=false;
        }
    }
}

// ============================================================
// Enemy AI
// ============================================================
function moveEnemy(e){
    var prefL=(e.type==="firefly");
    var d0=prefL?TURN_L[e.dir]:TURN_R[e.dir]; // preferred turn
    var d1=e.dir;                               // straight
    var d2=prefL?TURN_R[e.dir]:TURN_L[e.dir]; // opposite turn
    var d3=OPP[e.dir];                          // reverse

    var dirs=[d0,d1,d2,d3];
    for(var i=0;i<4;i++){
        var d=dirs[i];
        var nx=e.x+DX[d], ny=e.y+DY[d];
        if(!inBounds(nx,ny)) continue;
        if(_s.grid[ny][nx]!==EMPTY) continue;
        if(enemyAt(nx,ny)>=0) continue;
        e.dir=d; e.x=nx; e.y=ny;
        return;
    }
}

function enemyTick(){
    for(var i=0;i<_s.enemies.length;i++){
        moveEnemy(_s.enemies[i]);
        var pid=playerAt(_s.enemies[i].x,_s.enemies[i].y);
        if(pid!==null) triggerDeath(pid);
    }
}

function amoebaGrow(){
    var grew=false;
    var snapshot=_s.amoebaList.slice(); // avoid index drift while pushing
    for(var i=0;i<snapshot.length;i++){
        var a=snapshot[i];
        if(_s.grid[a.y][a.x]!==AMOEBA) continue;
        var d=Math.floor(rng()*4);
        var nx=a.x+DX[d], ny=a.y+DY[d];
        if(!inBounds(nx,ny)) continue;
        var t=_s.grid[ny][nx];
        if(t===EMPTY||t===DIRT){
            _s.grid[ny][nx]=AMOEBA;
            _s.amoebaList.push({x:nx,y:ny});
            grew=true;
        }
    }
    // Rebuild authoritative list
    var newL=[];
    for(var y=0;y<_s.CAVE_H;y++)
        for(var x=0;x<_s.CAVE_W;x++)
            if(_s.grid[y][x]===AMOEBA) newL.push({x:x,y:y});
    _s.amoebaList=newL;

    if(_s.amoebaList.length>AMOEBA_MAX){
        for(var ka=0;ka<_s.amoebaList.length;ka++) _s.grid[_s.amoebaList[ka].y][_s.amoebaList[ka].x]=BOULDER;
        _s.amoebaList=[];
        Game._ctx.chat("Amoeba too large - turned to boulders!");
    } else if(!grew && _s.amoebaList.length>0){
        for(var kb=0;kb<_s.amoebaList.length;kb++) _s.grid[_s.amoebaList[kb].y][_s.amoebaList[kb].x]=DIAMOND;
        Game._ctx.chat("Amoeba suffocated - turned to diamonds!");
        _s.amoebaList=[];
    }
}

// ============================================================
// Player actions
// ============================================================
function openExit(){
    _s.exitOpen=true;
    for(var y=0;y<_s.CAVE_H;y++)
        for(var x=0;x<_s.CAVE_W;x++)
            if(_s.grid[y][x]===EXIT_C) _s.grid[y][x]=EXIT_O;
    Game._ctx.chat("Exit open! Get to the X!");
}

function triggerDeath(playerID){
    var p=_s.pls[playerID];
    if(!p||p.dead||p.invulnTimer>0) return;
    p.dead=true;
    p.lives--;
    if(p.lives>0){
        p.respawnTimer=RESPAWN_TIME;
        Game._ctx.chatPlayer(playerID,"Crushed! Respawning in "+Math.ceil(RESPAWN_TIME)+"s ("+p.lives+" lives left)");
    } else {
        Game._ctx.chatPlayer(playerID,"No lives left! You are now spectating.");
    }
}

function tryMove(playerID, dx, dy){
    var p=_s.pls[playerID];
    if(!p||p.dead||p.exited||p.invulnTimer>0||_s.wonDelay>0) return;
    var tx=p.x+dx, ty=p.y+dy;
    if(!inBounds(tx,ty)) return;
    var t=_s.grid[ty][tx];

    if(t===EMPTY){
        p.x=tx; p.y=ty;
    } else if(t===DIRT){
        _s.grid[ty][tx]=EMPTY;
        p.x=tx; p.y=ty;
    } else if(t===DIAMOND){
        _s.grid[ty][tx]=EMPTY;
        p.x=tx; p.y=ty;
        p.diamonds++;
        p.score+=PTS_DIAMOND;
        _s.diamondsCollected++;
        if(_s.diamondsCollected>=_s.diamondsNeeded && !_s.exitOpen) openExit();
    } else if(t===EXIT_O){
        p.x=tx; p.y=ty;
        p.exited=true;
        p.score+=Math.floor(_s.timeLeft)*PTS_TIME+PTS_CAVE;
        checkCaveWon();
        return;
    } else if(t===BOULDER && dy===0){
        // Horizontal push: one empty space needed behind boulder
        var bx=tx+dx;
        if(inBounds(bx,ty) && _s.grid[ty][bx]===EMPTY && playerAt(bx,ty)===null && enemyAt(bx,ty)<0){
            _s.grid[ty][tx]=EMPTY;
            _s.grid[ty][bx]=BOULDER;
            _s.fallingGrid[ty][bx]=false;
            p.x=tx; p.y=ty;
        }
        return;
    } else {
        return;
    }

    // Check if player walked into an enemy
    if(enemyAt(p.x,p.y)>=0) triggerDeath(playerID);
}

function checkCaveWon(){
    for(var id in _s.pls){
        if(_s.pls[id].exited){
            _s.wonDelay=CAVE_WIN_DELAY;
            Game._ctx.chat("Cave "+_s.caveName+" cleared! Next cave in "+Math.ceil(CAVE_WIN_DELAY)+"s...");
            return;
        }
    }
}

// ============================================================
// Rendering
// ============================================================
function doRenderAscii(buf, playerID, ox, oy, width, height){
    var me=_s.pls[playerID];
    var px=me?me.x:_s.startX;
    var py=me?me.y:_s.startY;
    var camX=clamp(px-Math.floor(width/2), 0, Math.max(0,_s.CAVE_W-width));
    var camY=clamp(py-Math.floor(height/2), 0, Math.max(0,_s.CAVE_H-height));

    // Build quick lookup maps
    var eMap={};
    for(var i=0;i<_s.enemies.length;i++){
        var e=_s.enemies[i];
        eMap[e.x+","+e.y]=e;
    }
    var pMap={};
    for(var id in _s.pls){
        var p=_s.pls[id];
        if(!p.exited) pMap[p.x+","+p.y]=id;
    }

    var blink=Math.floor(_s.elapsed*2)%2===0;

    for(var sy=0;sy<height;sy++){
        for(var sx=0;sx<width;sx++){
            var gx=camX+sx, gy=camY+sy;
            if(!inBounds(gx,gy)){
                buf.setChar(sx,sy," ",null,null);
                continue;
            }
            var key=gx+","+gy;
            var ch=" ", fg=null, bg=null;

            // Base tile
            switch(_s.grid[gy][gx]){
                case EMPTY:      ch=" "; break;
                case DIRT:       ch=":"; fg=C_DIRT_FG; bg=C_DIRT_BG; break;
                case WALL:       ch="#"; fg=C_WALL_FG; bg=C_WALL_BG; break;
                case BOULDER:    ch="O"; fg=C_BOULDER; break;
                case DIAMOND:    ch=blink?"*":"+"; fg=blink?C_DIA_A:C_DIA_B; break;
                case EXIT_C:     ch="+"; fg=C_EXIT_C; break;
                case EXIT_O:     ch=blink?"X":"O"; fg=C_EXIT_O; break;
                case AMOEBA:     ch="~"; fg=C_AMOEBA; break;
                case MAGIC_WALL: ch="M"; fg=C_MAGIC_FG; bg=C_MAGIC_BG; break;
            }

            // Overlay: enemy
            if(eMap[key]){
                var en=eMap[key];
                if(en.type==="firefly"){  ch="/"; fg=C_FIREFLY;   bg=null; }
                else {                    ch="%"; fg=C_BUTTERFLY; bg=null; }
            }

            // Overlay: player
            if(pMap[key]){
                var pid2=pMap[key];
                var p2=_s.pls[pid2];
                if(p2.dead){ ch="x"; fg=C_DEAD_FG; bg=null; }
                else if(pid2===playerID){ ch="@"; fg=C_PLAYER; bg=null; }
                else { ch="@"; fg=C_OTHER; bg=null; }
            }

            buf.setChar(sx,sy,ch,fg,bg);
        }
    }
}

// ============================================================
// Results / player management
// ============================================================
function buildResults(){
    var arr=[];
    for(var id in _s.pls){
        var p=_s.pls[id];
        arr.push({name:p.name, score:p.score});
        if(p.score>_s.highScore) _s.highScore=p.score;
    }
    arr.sort(function(a,b){ return b.score-a.score; });
    var res=[];
    for(var i=0;i<arr.length;i++) res.push({name:arr[i].name, result:arr[i].score+" pts"});
    return res;
}

function addPlayer(id, name, team){
    if(_s.pls[id]) return;
    _s.pls[id]={
        name:name, team:team,
        x:_s.startX, y:_s.startY,
        lives:3, score:0, diamonds:0,
        exited:false, dead:false,
        respawnTimer:0, invulnTimer:INVULN_TIME
    };
    _s.plOrder.push(id);
}

// ============================================================
// Game object
// ============================================================
var Game = {
    gameName:  "Boulder Dash",
    teamRange: { min:1, max:4 },

    splashScreen:
        "=== BOULDER DASH ===\n" +
        "Dig through dirt, collect diamonds, reach the exit!\n\n" +
        "Controls: arrow keys to move / dig / push boulders\n" +
        "Collect enough diamonds to open the exit door [X]\n" +
        "Watch out for falling boulders and enemies!\n\n" +
        "  Firefly [/] — explodes into empty space when crushed\n" +
        "  Butterfly [%] — explodes into diamonds when crushed!\n" +
        "  Amoeba [~] — suffocate it for diamonds, or it turns to boulders\n" +
        "  Magic Wall [M] — boulders falling through become diamonds",

    init: function(ctx){
        return initialState();
    },

    begin: function(state, ctx){
        _s = state;
        _s.caveIndex=0; _s.elapsed=0; _s.gameEnded=false;
        _s.pls={}; _s.plOrder=[];
        var t = _s.teams || [];
        for(var i=0;i<t.length;i++){
            for(var j=0;j<t[i].players.length;j++){
                var p=t[i].players[j];
                addPlayer(p.id, p.name, t[i].name);
            }
        }
        loadCave(0);
        ctx.log("Boulder Dash started: cave 0, "+_s.plOrder.length+" players");
    },

    update: function(state, dt, events, ctx){
        _s = state;
        // Drain input/join/leave events first.
        for(var i=0;i<events.length;i++){
            var e = events[i];
            if(e.type === "join"){
                var t = _s.teams || [];
                for(var ti=0;ti<t.length;ti++){
                    for(var tj=0;tj<t[ti].players.length;tj++){
                        if(t[ti].players[tj].id === e.playerID){
                            addPlayer(e.playerID, t[ti].players[tj].name, t[ti].name);
                            ti = t.length;
                            break;
                        }
                    }
                }
            } else if(e.type === "leave"){
                delete _s.pls[e.playerID];
                for(var li=0;li<_s.plOrder.length;li++){
                    if(_s.plOrder[li]===e.playerID){ _s.plOrder.splice(li,1); break; }
                }
            } else if(e.type === "input"){
                var p = _s.pls[e.playerID];
                if(!p || p.dead) continue;
                if(e.key==="up")    tryMove(e.playerID, 0,-1);
                if(e.key==="down")  tryMove(e.playerID, 0, 1);
                if(e.key==="left")  tryMove(e.playerID,-1, 0);
                if(e.key==="right") tryMove(e.playerID, 1, 0);
            }
        }

        if(_s.gameEnded) return;
        _s.elapsed+=dt;
        _s.timeLeft-=dt;

        if(_s.wonDelay>0){
            _s.wonDelay-=dt;
            if(_s.wonDelay<=0){
                if(_s.caveIndex+1>=CAVES.length){
                    _s.gameEnded=true;
                    ctx.gameOver(buildResults());
                } else {
                    loadCave(_s.caveIndex+1);
                }
            }
            return;
        }

        if(_s.timeLeft<=0){
            _s.timeLeft=0; _s.gameEnded=true;
            ctx.chat("Time's up!");
            ctx.gameOver(buildResults());
            return;
        }

        var anyAlive=false;
        for(var id in _s.pls){ if(_s.pls[id].lives>0){ anyAlive=true; break; } }
        if(!anyAlive){
            _s.gameEnded=true;
            ctx.gameOver(buildResults());
            return;
        }

        for(var id in _s.pls){
            var p=_s.pls[id];
            if(p.dead && p.lives>0){
                p.respawnTimer-=dt;
                if(p.respawnTimer<=0){
                    p.dead=false;
                    p.x=_s.startX; p.y=_s.startY;
                    p.invulnTimer=INVULN_TIME;
                }
            }
            if(p.invulnTimer>0) p.invulnTimer-=dt;
        }

        if(_s.magicWallActive){
            _s.magicWallTimer-=dt;
            if(_s.magicWallTimer<=0){ _s.magicWallActive=false; ctx.chat("Magic walls have expired!"); }
        }

        _s.physicsTimer+=dt;
        while(_s.physicsTimer>=PHYSICS_INTERVAL){
            _s.physicsTimer-=PHYSICS_INTERVAL;
            physicsTick();
        }

        _s.enemyTimer+=dt;
        if(_s.enemyTimer>=ENEMY_INTERVAL){
            _s.enemyTimer-=ENEMY_INTERVAL;
            enemyTick();
        }

        if(_s.amoebaList.length>0){
            _s.amoebaTimer+=dt;
            if(_s.amoebaTimer>=AMOEBA_INTERVAL){
                _s.amoebaTimer-=AMOEBA_INTERVAL;
                amoebaGrow();
            }
        }
    },

    renderAscii: function(state, me, cells){
        _s = state;
        doRenderAscii(cells, me.id, 0, 0, cells.width, cells.height);
    },

    statusBar: function(state, me){
        _s = state;
        var p = _s.pls[me.id];
        if(!p) return "Boulder Dash";
        var need=Math.max(0, _s.diamondsNeeded-_s.diamondsCollected);
        var t=Math.max(0, Math.ceil(_s.timeLeft));
        var mw=_s.magicWallActive?" [Magic:"+Math.ceil(_s.magicWallTimer)+"s]":"";
        return "Cave "+_s.caveName+": "+_s.caveTitle+mw+
               "  | need:"+need+" got:"+p.diamonds+
               "  | lives:"+Math.max(0,p.lives)+
               "  | score:"+p.score+
               "  | "+t+"s";
    },

    commandBar: function(state, me){
        _s = state;
        var p = _s.pls[me.id];
        if(!p||p.dead) return "Boulder Dash — waiting to respawn...";
        return "[arrows] Move/Dig  [arrow into boulder] Push  [Enter] Chat";
    }
};
